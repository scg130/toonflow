package auth

import (
	"database/sql"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// Authenticate verifies username/password against o_user.
func Authenticate(db *sql.DB, username, password string) (userID, name string, err error) {
	var id, hash string
	err = db.QueryRow(
		"SELECT id, username, password FROM o_user WHERE username = ?",
		username,
	).Scan(&id, &name, &hash)
	if err == sql.ErrNoRows {
		return "", "", fmt.Errorf("invalid username or password")
	}
	if err != nil {
		return "", "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", "", fmt.Errorf("invalid username or password")
	}
	return id, name, nil
}

// SeedAdmin creates the default admin account if no users exist.
func SeedAdmin(db *sql.DB) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM o_user").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(DefaultAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		"INSERT INTO o_user (id, username, password) VALUES (?, ?, ?)",
		DefaultAdminID, DefaultAdminUsername, string(hash),
	)
	return err
}
