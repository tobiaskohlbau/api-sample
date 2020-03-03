package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/tobiaskohlbau/api-sample/api"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

type server struct {
	person *api.Person
	roles  []string
	r      *mux.Router
}

func main() {
	person := &api.Person{
		Id:       "b1d34a71-c9d6-4c1a-9278-fbcc1e22163a",
		Name:     "John Doe",
		Email:    "john@doe.com",
		Password: "sup3rs3cr3tpassw0rd",
	}

	srv := &server{
		// person mocks a database object
		person: person,
		// roles mocks user roles retrieved from authentication tokens or similar
		roles: []string{"USER"},
		r:     mux.NewRouter(),
	}

	srv.r.HandleFunc("/person", srv.GetPersonHandler).Methods("GET")
	srv.r.HandleFunc("/person", srv.UpdatePersonHandler).Methods("POST")

	log.Fatal(http.ListenAndServe(":8080", srv))
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.r.ServeHTTP(w, r)
}

func (s *server) GetPersonHandler(w http.ResponseWriter, r *http.Request) {
	redactResponse(s.person, s.roles)
	fmt.Fprintf(w, protojson.Format(s.person))
}

func (s *server) UpdatePersonHandler(w http.ResponseWriter, r *http.Request) {
	requestData, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Body.Close()

	person := &api.Person{}
	if err := protojson.Unmarshal(requestData, person); err != nil {
		log.Println(err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	redactRequest(person.GetUpdateMask().GetPaths(), "", person, s.person, s.roles)

	redactResponse(s.person, s.roles)

	fmt.Fprintf(w, protojson.Format(s.person))
}

func redactRequest(paths []string, pathPrefix string, input proto.Message, output proto.Message, roles []string) {
	inputReflect := input.ProtoReflect()
	outputReflect := output.ProtoReflect()
	inputReflect.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() == protoreflect.MessageKind {
			if fd.Message().FullName() == "google.protobuf.FieldMask" {
				return true
			}
			pathPrefix = pathPrefix + string(fd.Name()) + "."
			redactRequest(paths, pathPrefix, v.Message().Interface(), outputReflect.Mutable(fd).Message().Interface(), roles)
			return true
		}
		opts := fd.Options().(*descriptorpb.FieldOptions)

		if proto.GetExtension(opts, api.E_Readonly).(bool) {
			return true
		}

		for _, path := range paths {
			fieldPath := pathPrefix + string(fd.Name())
			if path != fieldPath {
				continue
			}

			if !proto.HasExtension(opts, api.E_Role) {
				outputReflect.Set(fd, v)
				return true
			}

			requiredRole := proto.GetExtension(opts, api.E_Role).(string)
			for _, role := range roles {
				if role == requiredRole {
					outputReflect.Set(fd, v)
					return true
				}
			}

			return true
		}

		return true
	})
}

func redactResponse(pb proto.Message, roles []string) {
	m := pb.ProtoReflect()
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		opts := fd.Options().(*descriptorpb.FieldOptions)

		if !proto.HasExtension(opts, api.E_Role) {
			return true
		}

		requiredRole := proto.GetExtension(opts, api.E_Role).(string)
		for _, role := range roles {
			if role == requiredRole {
				return true
			}
		}

		m.Clear(fd)
		return true
	})
}
