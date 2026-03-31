package db

import (
	"fmt"
	"os"

	"github.com/golang/glog"
	"github.com/jmoiron/sqlx"
)

var Db *dbModule

type dbModule struct {
	db *sqlx.DB
}

func NewDbModule() error {

	dbHost := os.Getenv("POSTGRES_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}

	dbPort := os.Getenv("POSTGRES_PORT")
	if dbPort == "" {
		dbPort = "5432"
	}

	dbName := os.Getenv("POSTGRES_DB")
	if dbName == "" {
		dbName = "history"
	}

	dbUser := os.Getenv("POSTGRES_USER")
	if dbUser == "" {
		dbUser = "postgres"
	}

	dbPassword := os.Getenv("POSTGRES_PASSWORD")
	if dbPassword == "" {
		dbPassword = "password"
	}

	// Build connection string
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	db, err := sqlx.Connect("postgres", connStr)
	if err != nil {
		glog.Errorf("Failed to connect to PostgreSQL: %v", err)
		return err
	}

	if err := db.Ping(); err != nil {
		glog.Errorf("Failed to ping PostgreSQL: %v", err)
		return err
	}

	Db = &dbModule{
		db: db,
	}

	if err := Db.initSourceSchema(); err != nil {
		glog.Errorf("Database init \"sources\" schema error: %v", err)
		return err
	}

	if err := Db.initAppSchema(); err != nil {
		glog.Errorf("Database init \"apps\" schema error: %v", err)
		return err
	}

	if err := Db.initStateSchema(); err != nil {
		glog.Errorf("Database init \"app_states\" schema error: %v", err)
		return err
	}

	return nil
}
