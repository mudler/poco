FROM golang:alpine as builder

COPY . /code

RUN cd /code && CGO_ENABLED=0 go build 

FROM golang:alpine

COPY --from=builder /code/poco /usr/bin/poco