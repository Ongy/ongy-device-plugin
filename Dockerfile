FROM docker.io/golang:1.19.2-alpine3.16
RUN apk --no-cache add git pkgconfig build-base
RUN mkdir -p /go/src/github.com/ongy/k8s-device-plugin
ADD . /go/src/github.com/ongy/k8s-device-plugin
WORKDIR /go/src/github.com/ongy/k8s-device-plugin/cmd/k8s-device-plugin
RUN go install \
    -ldflags="-X main.gitDescribe=$(git -C /go/src/github.com/ongy/k8s-device-plugin/ describe --always --long --dirty)" 

FROM alpine:3.16
WORKDIR /root/
COPY --from=0 /go/bin/k8s-device-plugin .
CMD ["./k8s-device-plugin", "-logtostderr=true", "-stderrthreshold=INFO", "-v=5"]
