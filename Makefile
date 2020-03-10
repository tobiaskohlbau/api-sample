build:
	go build

run: build
	./api-sample

protobuf:
	protoc --proto_path=mongo --go_out=mongo --go_opt=paths=source_relative mongo/mongo.proto
	protoc --proto_path=api --proto_path=mongo --go_out=api --go_opt=paths=source_relative api/api.proto