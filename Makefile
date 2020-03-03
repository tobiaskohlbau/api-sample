build:
	go build

run: build
	./api-sample

protobuf:
	protoc --proto_path=api --go_out=api --go_opt=paths=source_relative api/api.proto

test:
	@curl --location --request POST 'http://localhost:8080/person' --header 'Content-Type: application/json' \
		--data-raw '{"id": "NONE", "name": "Jane Doe", "email": "jane@doe.com", "password": "supernonsecretpassword", "contact": {"mobilePhone": "12345678"}, "updateMask": "id,name,email,password,contact.mobilePhone"}'
