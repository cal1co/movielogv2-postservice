package handlers

import "github.com/gocql/gocql"

type Session interface {
	Query(stmt string, values ...interface{}) *gocql.Query
}

type Handler struct {
	Session *gocql.Session
}
