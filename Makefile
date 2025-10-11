# Makefile for Go Video Processing App

.PHONY: run build test clean

run:
	docker-compose -f infra/docker-compose.yml up --build

build:
	cd backend && go build -o ../bin/backend ./...
	cd worker && go build -o ../bin/worker ./...

test:
	cd backend && go test ./...
	cd worker && go test ./...

clean:
	rm -rf bin
	docker-compose -f infra/docker-compose.yml down -v
