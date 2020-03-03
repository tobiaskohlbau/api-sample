Sample

```
curl --location --request POST 'http://localhost:8080/person' \
--header 'Content-Type: application/json' \
--data-raw '{
	"id": "NONE",
	"name": "Jane Doe",
	"email": "jane@doe.com",
	"password": "supernonsecretpassword",
	"contact": {
		"mobilePhone": "12345678"
	},
	"updateMask": "id,name,email,password,contact.mobilePhone"
}'
```

or 
```
make test
```