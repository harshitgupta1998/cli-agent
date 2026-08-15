FROM golang:1.25-alpine

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

RUN go build -o /usr/local/bin/termind-api ./cmd/server

EXPOSE 8000

CMD ["termind-api"]
