package app

import (
	"context"
	"net/http"
)

type contextKey string

const userIDKey contextKey = "userID"
const userRoleKey contextKey = "userRole"

func withUser(ctx context.Context, id int64, role string) context.Context {
	ctx = context.WithValue(ctx, userIDKey, id)
	return context.WithValue(ctx, userRoleKey, role)
}

func userID(r *http.Request) int64 {
	id, _ := r.Context().Value(userIDKey).(int64)
	return id
}

func userRole(r *http.Request) string {
	role, _ := r.Context().Value(userRoleKey).(string)
	return role
}
