FROM golang:1.17-alpine

RUN apk add make gcc musl-dev linux-headers

ENV PROOT=/go/src/tiechian/main-chain
ENV GO111MODULE=on GOPROXY="https://goproxy.cn"

COPY . $PROOT/

RUN cd $PROOT/ && go build -o tie

FROM alpine

ENV PROOT=/go/src/tiechian/main-chain
COPY --from=0  $PROOT/tie /usr/bin
WORKDIR /data

CMD ["tie"]