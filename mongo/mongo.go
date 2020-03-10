package mongo

import (
	"errors"
	"fmt"
	reflect "reflect"
	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/bson/bsoncodec"
	"go.mongodb.org/mongo-driver/bson/bsonrw"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func Decoder(dc bsoncodec.DecodeContext, vr bsonrw.ValueReader, val reflect.Value) error {
	if !val.IsValid() || !val.CanSet() || val.Kind() != reflect.Struct {
		return bsoncodec.ValueDecoderError{
			Name:     "mongoDecodeValue",
			Kinds:    []reflect.Kind{reflect.Struct},
			Received: val,
		}
	}

	protoMessage := val.Addr().MethodByName("ProtoReflect").Call(nil)[0].Interface().(protoreflect.Message)

	dr, err := vr.ReadDocument()
	if err != nil {
		return fmt.Errorf("failed to read document: %w", err)
	}

	for {
		name, vr, err := dr.ReadElement()
		if errors.Is(err, bsonrw.ErrEOD) {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read element: %w", err)
		}

		fieldValue, options, ok := findField(reflect.TypeOf(val.Interface()), val, protoMessage.Descriptor().Fields(), name)
		if !ok {
			fmt.Printf("searched for %s but did not found it\n", name)
			continue
		}

		switch options.GetType() {
		case MongoType_MONGO_TYPE_OBJECT_ID:
			oid, err := vr.ReadObjectID()
			if err != nil {
				return fmt.Errorf("failed to read ObjectId: %w", err)
			}
			fieldValue.Set(reflect.ValueOf(oid.Hex()))
			continue
		}

		underlyingDecoder, err := dc.LookupDecoder(reflect.TypeOf(fieldValue.Interface()))
		if err != nil {
			return fmt.Errorf("failed to find underlying decoder: %w", err)
		}

		err = underlyingDecoder.DecodeValue(dc, vr, fieldValue)
		if err != nil {
			return fmt.Errorf("failed to decode via underlying decoder: %w", err)
		}
	}

	return nil
}

func Encoder(ec bsoncodec.EncodeContext, vw bsonrw.ValueWriter, val reflect.Value) error {
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return bsoncodec.ValueEncoderError{
			Name:     "mongoEncodeValue",
			Kinds:    []reflect.Kind{reflect.Struct},
			Received: val,
		}
	}

	dw, err := vw.WriteDocument()
	if err != nil {
		return fmt.Errorf("failed to write document: %w", err)
	}

	protoMessage := val.Addr().MethodByName("ProtoReflect").Call(nil)[0].Interface().(protoreflect.Message)

	var loggedError error
	protoMessage.Range(func(fd protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		opts := fd.Options().(*descriptorpb.FieldOptions)
		options := proto.GetExtension(opts, E_Options).(*MongoOptions)

		fieldName := fd.JSONName()
		if options.GetName() != "" {
			fieldName = options.GetName()
		}

		dew, err := dw.WriteDocumentElement(fieldName)
		if err != nil {
			loggedError = fmt.Errorf("failed to write doucment element: %w", err)
			return false
		}

		if options == nil {
			underlyingEncoder, err := ec.LookupEncoder(reflect.TypeOf(value.Interface()))
			if err != nil {
				loggedError = fmt.Errorf("failed to lookup encoder: %w", err)
				return false
			}

			err = underlyingEncoder.EncodeValue(ec, dew, reflect.ValueOf(value.Interface()))
			if err != nil {
				loggedError = fmt.Errorf("failed to encode via underlying encoder: %w", err)
				return false
			}

			return true
		}

		switch options.GetType() {
		case MongoType_MONGO_TYPE_OBJECT_ID:
			oid, err := primitive.ObjectIDFromHex(value.String())
			if err != nil {
				loggedError = fmt.Errorf("failed to convert object id: %w", err)
				return false
			}
			if err := dew.WriteObjectID(oid); err != nil {
				loggedError = fmt.Errorf("failed to write object id: %w", err)
				return false
			}
		}

		return true
	})

	if err := dw.WriteDocumentEnd(); err != nil {
		return fmt.Errorf("failed to write document end: %w", err)
	}

	return loggedError
}

func findField(typ reflect.Type, val reflect.Value, fields protoreflect.FieldDescriptors, name string) (reflect.Value, *MongoOptions, bool) {
	for i := 0; i < val.NumField(); i++ {
		structField := typ.Field(i)
		valueField := val.Field(i)
		protobuf, ok := structField.Tag.Lookup("protobuf")
		if !ok {
			continue
		}

		protobufItems := strings.Split(protobuf, ",")

		protoFieldNumber, err := strconv.Atoi(protobufItems[1])
		if err != nil {
			return reflect.Value{}, nil, false
		}

		fd := fields.ByNumber(protoreflect.FieldNumber(protoFieldNumber))

		opts := fd.Options().(*descriptorpb.FieldOptions)
		mongoOptions := proto.GetExtension(opts, E_Options).(*MongoOptions)

		if mongoOptions.GetName() == name {
			return valueField, mongoOptions, true
		}

		fieldName := string(fd.JSONName())
		if fieldName == name {
			return valueField, mongoOptions, true
		}
	}

	return reflect.Value{}, nil, false
}
