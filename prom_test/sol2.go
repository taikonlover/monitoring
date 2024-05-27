package prom_test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var nodes *v1.NodeList
var metricsClient *metricsv1beta1.MetricsV1beta1Client
var Client *kubernetes.Clientset

type Data struct {
	Value float32
	Time  time.Time
}

type NodeFunc struct {
	Node      v1.Node
	nodestate *NodeState
}

func (n NodeFunc) CpuUsage() float64 {
	return n.nodestate.CpuUsage
}
func (n NodeFunc) MemUsage() float64 {
	return n.nodestate.MemoryUsage
}
func (n NodeFunc) Storage() float64 {
	return n.nodestate.StorageUsage
}

func (n NodeFunc) CPU() int64 {
	return n.nodestate.CPU
}

func (n NodeFunc) MEMORY() int64 {
	return n.nodestate.MEMORY
}

func (n NodeFunc) Pods(namespace string) (podL []*v1.Pod) {
	pods, err := Client.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	for _, pod := range pods.Items {
		if pod.Spec.NodeName == n.Node.Name {
			podL = append(podL, &pod)
		}
	}
	return
}

type NodeState struct {
	Node         v1.Node
	CpuUsage     float64
	MemoryUsage  float64
	StorageUsage float64
	CPU          int64 // 单位：Cores
	MEMORY       int64 // 单位：B
	CpuGuage     prometheus.Gauge
	MemoryGuage  prometheus.Gauge
	StorageGuage prometheus.Gauge
}

func init() {
	http.Handle("/metrics", promhttp.Handler())

	go func() {
		http.ListenAndServe(":2112", nil)
	}()
	nodes, metricsClient = getService()
}

func getService() (nodes *v1.NodeList, metricsClient *metricsv1beta1.MetricsV1beta1Client) {
	var config *rest.Config
	var err error

	// 从KUBECONFIG环境变量或~/.kube/config文件中加载配置
	if os.Getenv("KUBECONFIG") != "" {
		config, err = clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	} else {
		config, err = rest.InClusterConfig()
	}

	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	Client = clientset
	if err != nil {
		panic(err.Error())
	}

	// 获取所有节点信息
	nodes, err = clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	metricsClient, err = metricsv1beta1.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	return
}

func GetState() (getf []*NodeFunc, server func()) {
	var nodestate []*NodeState
	for _, node := range nodes.Items {
		var nodeS *NodeState = new(NodeState)
		var nodeF *NodeFunc = new(NodeFunc)
		nodeF.nodestate = nodeS
		nodeS.Node = node
		nodeF.Node = node
		nodeF.nodestate.CPU = nodeF.Node.Status.Capacity.Cpu().Value()
		nodeF.nodestate.MEMORY = nodeF.Node.Status.Capacity.Memory().Value()
		nodeName := node.Name
		nodeS.CpuGuage = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: strings.ReplaceAll(nodeName, "-", "_") + "_cpu_usage",
				Help: "Current CPU usage",
			})
		nodeS.MemoryGuage = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: strings.ReplaceAll(nodeName, "-", "_") + "_Mem_usage",
				Help: "Current Mem usage",
			})
		nodeS.StorageGuage = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: strings.ReplaceAll(nodeName, "-", "_") + "_storage_usage",
				Help: "Current storage usage",
			})
		prometheus.MustRegister(nodeS.CpuGuage)
		prometheus.MustRegister(nodeS.MemoryGuage)
		prometheus.MustRegister(nodeS.StorageGuage)
		nodestate = append(nodestate, nodeS)
		getf = append(getf, nodeF)
	}

	server = func() {
		for _, nodes := range nodestate {
			go func() {
				for {
					nodeName := nodes.Node.Name
					usage, err := metricsClient.NodeMetricses().Get(context.Background(), nodeName, metav1.GetOptions{})
					if err != nil {
						panic(err.Error())
					}
					cpuPercent := usage.Usage.Cpu().AsApproximateFloat64() / nodes.Node.Status.Capacity.Cpu().AsApproximateFloat64() * 100
					nodes.CpuGuage.Set(cpuPercent)
					nodes.CpuUsage = cpuPercent

					MemPercent := usage.Usage.Memory().AsApproximateFloat64() / nodes.Node.Status.Capacity.Memory().AsApproximateFloat64() * 100
					nodes.MemoryGuage.Set(MemPercent)
					nodes.MemoryUsage = MemPercent

					storagePercent := usage.Usage.Storage().AsApproximateFloat64()
					nodes.StorageGuage.Set(storagePercent)
					nodes.StorageUsage = storagePercent
					time.Sleep(time.Second)
				}

			}()
		}

	}
	return
}

/*
使用说明：
采用闭包的形式提供接口
调用prom_test包后
调用GetState()函数，返回提供服务的函数server()，和一个传递数据的指针切片getf
getf有各个节点的信息和获取cpu和Mem的函数接口
使用时，先创建一个协程运行server()函数，需要数据时调用函数即可获取数据
*/
