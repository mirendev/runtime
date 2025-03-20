package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/pkg/controller"
)

// UserEntity represents a user in the system
type UserEntity struct {
	controller.BaseEntity
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

// UserEntityFactory creates new UserEntity instances
type UserEntityFactory struct{}

func (f *UserEntityFactory) Create() controller.Entity {
	return &UserEntity{
		BaseEntity: controller.BaseEntity{
			Kind: "User",
		},
	}
}

// UserController handles user entities
type UserController struct {
	controller controller.Controller
}

// NewUserController creates a new user controller
func NewUserController(store controller.EntityStore) *UserController {
	handler := func(ctx context.Context, entity controller.Entity) error {
		user, ok := entity.(*UserEntity)
		if !ok {
			return fmt.Errorf("entity is not a UserEntity")
		}

		log.Printf("Processing user: %s, Name: %s, Email: %s", user.ID, user.Name, user.Email)

		// Here you would implement your business logic for handling users
		// For example, synchronizing with an external system

		return nil
	}

	reconcileController := controller.NewReconcileController(
		"user-controller",
		"User",
		store,
		handler,
		1*time.Minute, // Resync every minute
		2,             // 2 worker goroutines
	)

	return &UserController{
		controller: reconcileController,
	}
}

// Start starts the controller
func (c *UserController) Start(ctx context.Context) error {
	return c.controller.Start(ctx)
}

// Stop stops the controller
func (c *UserController) Stop() {
	c.controller.Stop()
}

func main() {
	// Connect to etcd
	etcdClient, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to connect to etcd: %v", err)
	}
	defer etcdClient.Close()

	// Register entity factories
	factories := map[string]controller.EntityFactory{
		"User": &UserEntityFactory{},
	}

	// Create entity store
	store := controller.NewEtcdEntityStore(etcdClient, factories, "/entities")

	// Create controller manager
	manager := controller.NewControllerManager()

	// Create and add user controller
	userController := NewUserController(store)
	manager.AddController(userController)

	// Create a context that will be canceled on SIGINT or SIGTERM
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signalCh
		log.Println("Received shutdown signal")
		cancel()
	}()

	// Start controllers
	if err := manager.Start(ctx); err != nil {
		log.Fatalf("Failed to start controllers: %v", err)
	}

	// Example: Create a user
	user := &UserEntity{
		BaseEntity: controller.BaseEntity{
			ID:      "user-123",
			Kind:    "User",
			Version: "v1",
		},
		Name:  "John Doe",
		Email: "john.doe@example.com",
		Age:   30,
	}

	if err := store.Create(ctx, user); err != nil {
		log.Printf("Failed to create user: %v", err)
	} else {
		log.Printf("Created user: %s", user.ID)
	}

	// Wait for context cancelation
	<-ctx.Done()

	// Stop controllers
	manager.Stop()
	log.Println("Controllers stopped, exiting")
}
