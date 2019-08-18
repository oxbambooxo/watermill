package main

import (
	"context"
	stdSQL "database/sql"
	"encoding/json"
	"log"
	"time"

	driver "github.com/go-sql-driver/mysql"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-kafka/pkg/kafka"
	"github.com/ThreeDotsLabs/watermill-sql/pkg/sql"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/ThreeDotsLabs/watermill/message/router/plugin"
)

var (
	logger     = watermill.NewStdLogger(false, false)
	kafkaTopic = "events"
	mysqlTable = "events"
)

func main() {
	router, err := message.NewRouter(message.RouterConfig{}, logger)
	if err != nil {
		panic(err)
	}

	router.AddPlugin(plugin.SignalsHandler)
	router.AddMiddleware(middleware.Recoverer)

	db := createDB()

	subscriber := createSubscriber(db)
	publisher := createPublisher()

	router.AddHandler(
		"mysql-to-kafka",
		mysqlTable,
		subscriber,
		kafkaTopic,
		publisher,
		func(msg *message.Message) ([]*message.Message, error) {
			consumedEvent := event{}
			err := json.Unmarshal(msg.Payload, &consumedEvent)
			if err != nil {
				return nil, err
			}

			log.Printf("received event %+v with UUID %s", consumedEvent, msg.UUID)

			return []*message.Message{msg}, nil
		},
	)

	go func() {
		<-router.Running()
		simulateEvents(db)
	}()

	if err := router.Run(context.Background()); err != nil {
		panic(err)
	}
}

func createDB() *stdSQL.DB {
	conf := driver.NewConfig()
	conf.Net = "tcp"
	conf.User = "root"
	conf.Addr = "mysql"
	conf.DBName = "watermill"

	db, err := stdSQL.Open("mysql", conf.FormatDSN())
	if err != nil {
		panic(err)
	}

	err = db.Ping()
	if err != nil {
		panic(err)
	}

	return db
}

func createSubscriber(db *stdSQL.DB) message.Subscriber {
	pub, err := sql.NewSubscriber(
		db,
		sql.SubscriberConfig{
			SchemaAdapter:    sql.DefaultSchema{},
			OffsetsAdapter:   sql.DefaultMySQLOffsetsAdapter{},
			InitializeSchema: true,
		},
		logger,
	)
	if err != nil {
		panic(err)
	}

	return pub
}

func createPublisher() message.Publisher {
	pub, err := kafka.NewPublisher(
		[]string{"kafka:9092"},
		kafka.DefaultMarshaler{},
		nil,
		logger,
	)
	if err != nil {
		panic(err)
	}

	return pub
}

type event struct {
	Name       string `json:"name"`
	OccurredAt string `json:"occurred_at"`
}

func simulateEvents(db *stdSQL.DB) {
	pub, err := sql.NewPublisher(db, sql.PublisherConfig{
		SchemaAdapter: sql.DefaultSchema{},
	}, logger)
	if err != nil {
		panic(err)
	}

	for {
		e := event{
			Name:       "UserSignedUp",
			OccurredAt: time.Now().UTC().Format(time.RFC3339),
		}
		payload, err := json.Marshal(e)
		if err != nil {
			panic(err)
		}

		err = pub.Publish(mysqlTable, message.NewMessage(
			watermill.NewUUID(),
			payload,
		))
		if err != nil {
			panic(err)
		}

		time.Sleep(time.Second)
	}
}