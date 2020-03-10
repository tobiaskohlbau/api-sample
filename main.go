package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"reflect"
	"time"

	"github.com/gorilla/mux"
	"github.com/tobiaskohlbau/api-sample/api"
	mongov1 "github.com/tobiaskohlbau/api-sample/mongo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsoncodec"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

type server struct {
	db     *mongo.Client
	person *api.Person
	roles  []string
	r      *mux.Router
}

func main() {
	rb := bsoncodec.NewRegistryBuilder()

	defaultValueDecoders := bsoncodec.DefaultValueDecoders{}
	defaultValueEncoders := bsoncodec.DefaultValueEncoders{}

	defaultValueDecoders.RegisterDefaultDecoders(rb)
	defaultValueEncoders.RegisterDefaultEncoders(rb)

	t := reflect.TypeOf((*proto.Message)(nil)).Elem()
	rb.RegisterHookDecoder(t, bsoncodec.ValueDecoderFunc(mongov1.Decoder))
	rb.RegisterHookEncoder(t, bsoncodec.ValueEncoderFunc(mongov1.Encoder))

	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017").SetRegistry(rb.Build()))
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = client.Connect(ctx)
	if err != nil {
		log.Fatal(err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatal(err)
	}

	srv := &server{
		db: client,
		// roles mocks user roles retrieved from authentication tokens or similar
		roles: []string{"USER"},
		r:     mux.NewRouter(),
	}

	srv.r.HandleFunc("/person/{id:[0-9a-z-]+}", srv.GetPersonHandler).Methods("GET")
	srv.r.HandleFunc("/person", srv.UpsertPersonHandler).Methods("POST")
	srv.r.HandleFunc("/person/{id:[0-9a-z-]+}", srv.UpsertPersonHandler).Methods("PATCH")

	log.Fatal(http.ListenAndServe(":8080", srv))
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.r.ServeHTTP(w, r)
}

func (s *server) GetPersonHandler(w http.ResponseWriter, r *http.Request) {
	collection := s.db.Database("test").Collection("test")

	vars := mux.Vars(r)
	id := vars["id"]

	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		log.Println(err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	person := &api.Person{}
	if err := collection.FindOne(r.Context(), bson.M{"_id": oid}).Decode(person); err != nil {
		log.Println(err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	redactResponse(person, s.roles)
	fmt.Fprintf(w, protojson.Format(person))
}

func (s *server) UpsertPersonHandler(w http.ResponseWriter, r *http.Request) {
	requestData, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Body.Close()

	request := &api.PersonRequest{}
	if err := protojson.Unmarshal(requestData, request); err != nil {
		log.Println("failed to unmarshal request:", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	redactedRequest := &api.PersonRequest{}
	redactRequest(request.GetUpdateMask().GetPaths(), "", request, redactedRequest, s.roles)
	person := redactedRequest.GetPerson()

	vars := mux.Vars(r)
	id := vars["id"]

	oid := primitive.NewObjectID()
	if id != "" {
		oid, err = primitive.ObjectIDFromHex(id)
		if err != nil {
			log.Println("invalid objectid:", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
	}
	person.Id = oid.Hex()

	collection := s.db.Database("test").Collection("test")
	fmt.Println(person)
	update := bson.M{"$set": person, "$inc": bson.M{"test": 1}}
	result, err := collection.UpdateOne(r.Context(), bson.M{"_id": oid, "test": 1}, update, options.Update().SetUpsert(true))
	if err != nil {
		log.Println("failed to updateOne:", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if result.UpsertedID != nil {
		person.Id = result.UpsertedID.(primitive.ObjectID).Hex()
	}
	redactResponse(person, s.roles)
	fmt.Fprintf(w, protojson.Format(person))
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
