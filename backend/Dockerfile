FROM golang:1.25

WORKDIR /app

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates ffmpeg python3 python3-pip \
  && ln -s /usr/bin/python3 /usr/local/bin/python \
  && rm -rf /var/lib/apt/lists/*

COPY requirements-stt.txt ./
RUN pip install --break-system-packages --no-cache-dir -r requirements-stt.txt

COPY go.mod ./
RUN go mod download

COPY . .

RUN go build -o /usr/local/bin/termind-api ./cmd/server

ENV TERMIND_STT_MODEL=tiny.en
ENV TERMIND_STT_MODEL_DIR=/models/whisper
ENV TERMIND_STT_DEVICE=cpu
ENV TERMIND_STT_COMPUTE_TYPE=int8

EXPOSE 8000

CMD ["termind-api"]
