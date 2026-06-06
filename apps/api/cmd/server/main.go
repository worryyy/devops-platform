package main

import (
	"context"
	"log"

	"campus-forum/internal/config"
	"campus-forum/internal/database"
	"campus-forum/internal/modules/comment"
	"campus-forum/internal/modules/session"
	"campus-forum/internal/modules/topic"
	"campus-forum/internal/modules/user"
	"campus-forum/internal/router"
)

func main() {
	ctx := context.Background()
	cfg := config.Load()

	mysqlDB, err := database.NewMySQL(cfg)
	if err != nil {
		log.Fatalf("init mysql: %v", err)
	}
	if err := database.PingMySQL(ctx, mysqlDB); err != nil {
		log.Fatalf("ping mysql: %v", err)
	}

	mongoClient, mongoDB, err := database.NewMongo(ctx, cfg)
	if err != nil {
		log.Fatalf("init mongo: %v", err)
	}
	defer func() {
		if err := mongoClient.Disconnect(context.Background()); err != nil {
			log.Printf("disconnect mongo: %v", err)
		}
	}()

	userRepo := user.NewRepository(mysqlDB)
	if err := userRepo.AutoMigrate(); err != nil {
		log.Fatalf("migrate users table: %v", err)
	}

	topicRepo := topic.NewRepository(mongoDB)
	if err := topicRepo.EnsureIndexes(ctx); err != nil {
		log.Fatalf("init topic indexes: %v", err)
	}

	commentRepo := comment.NewRepository(mongoDB)
	if err := commentRepo.EnsureIndexes(ctx); err != nil {
		log.Fatalf("init comment indexes: %v", err)
	}

	userService := user.NewService(userRepo)
	topicService := topic.NewService(topicRepo, userService)
	commentService := comment.NewService(commentRepo, userService, topicService)
	sessionService := session.NewService(userService)

	engine := router.New(router.Dependencies{
		UserService:    userService,
		SessionService: sessionService,
		TopicService:   topicService,
		CommentService: commentService,
		MySQL:           mysqlDB,
		Mongo:           mongoDB,
	})

	addr := ":" + cfg.AppPort
	log.Printf("campus forum server listening on %s", addr)
	if err := engine.Run(addr); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
