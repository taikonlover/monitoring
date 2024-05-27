package main

import (
	"monitoring/prom_test"
)

func main() {
	_, server := prom_test.GetState()
	go server()
	select {}
}
