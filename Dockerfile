FROM golang:1.19.6

WORKDIR /app

ADD . /app

RUN go mod download

RUN go build -o yuzu-post-handler

CMD ["./yuzu-post-handler"]

EXPOSE 8081
