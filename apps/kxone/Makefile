.PHONY: help dev build test clean worker

help:
	@echo "可用命令："
	@echo "  make dev        - 启动开发服务（HTTP API）"
	@echo "  make worker     - 启动 Worker（定时任务）"
	@echo "  make build      - 编译二进制"
	@echo "  make test       - 运行测试"
	@echo "  make clean      - 清理构建产物"

dev:
	go run cmd/server/main.go

worker:
	go run cmd/worker/main.go

build:
	go build -o bin/kxone cmd/server/main.go
	go build -o bin/kxone-worker cmd/worker/main.go

test:
	go test -v ./...

clean:
	rm -rf bin/