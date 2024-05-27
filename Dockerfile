FROM busybox

COPY main /usr/local/bin/main

RUN --network=host

EXPOSE 2112

CMD [ "main" ]