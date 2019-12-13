package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	"github.com/lib/pq"
	"log"
	"os"
)

var db *sql.DB
var config map[string]string

const (
	db_host    = "DB_HOST"
	db_port    = "DB_PORT"
	db_user    = "DB_USER"
	db_pass    = "DB_PASS"
	db_name    = "DB_NAME"
	table_name = "TABLE_NAME"
)

type User struct {
	ID         int            `json:"id, Number"`
	TelegramID string         `json:"telegram_id"`
	Accounts   pq.StringArray `json:"accounts"`
	Editing    bool           `json:"editing"`
	Adding     bool           `json:"adding"`
	LastCheck  string         `json:"last_check"`
	Settings   Settings       `json:"settings"`
}

type Settings struct {
	Notification string      `json:"notification"`
	MessageID    json.Number `json:"message_id, Number"`
}

func init() {
	config = dbConfig()
	var err error
	psqlInfo := fmt.Sprintf("host=%s port=%s user=%s "+
		"password=%s dbname=%s sslmode=disable",
		config[db_host], config[db_port],
		config[db_user], config[db_pass], config[db_name])

	db, err = sql.Open("postgres", psqlInfo)
	if err != nil {
		panic(err)
	}
	err = db.Ping()
	if err != nil {
		panic(err)
	}
}

func GetUser(telegram_id string) (User, error) {
	u := User{}

	query := `
        SELECT *
        FROM ` + config[table_name] + `
        WHERE telegram_id = $1;`

	row := db.QueryRow(query, telegram_id)

	err := row.Scan(&u.ID, &u.TelegramID, &u.Accounts, &u.Editing, &u.LastCheck, &u.Adding, &u.Settings)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Print(err)
		}
	}

	return u, err
}

func GetActiveUsers(inactive string) ([]User, error) {
	users := []User{}

	query := `
        SELECT *
        FROM ` + config[table_name] + `
        WHERE settings->>'notification' != $1
        AND array_length(accounts, 1) > 0;`

	rows, err := db.Query(query, inactive)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		u := User{}
		err = rows.Scan(&u.ID, &u.TelegramID, &u.Accounts, &u.Editing, &u.LastCheck, &u.Adding, &u.Settings)
		if err != nil {
			return users, err
		}

		users = append(users, u)
	}

	return users, err
}

func InsertUser(u User) {
	query := `
        INSERT INTO ` + config[table_name] + ` (telegram_id, editing, adding, accounts, last_check, settings)
        VALUES ($1, $2, $3, $4, $5, $6)`

	settings, _ := json.Marshal(u.Settings)

	_, err := db.Exec(query, u.TelegramID, u.Editing, u.Adding, u.Accounts, u.LastCheck, settings)
	if err != nil {
		panic(err)
	}
}

func UpdateUserEditing(telegram_id string, editing bool, adding bool) {
	query := `
        UPDATE ` + config[table_name] + `
        SET editing = $2, adding = $3
        WHERE telegram_id = $1`
	_, err := db.Exec(query, telegram_id, editing, adding)
	if err != nil {
		panic(err)
	}
}

func UpdateUserAccounts(telegram_id string, accounts []string) {
	query := `
        UPDATE ` + config[table_name] + `
        SET accounts = $2
        WHERE telegram_id = $1`
	_, err := db.Exec(query, telegram_id, pq.StringArray(accounts))
	if err != nil {
		panic(err)
	}
}

func UpdateSettings(telegram_id string, s Settings) {
	query := `
        UPDATE ` + config[table_name] + `
        SET settings = $2
        WHERE telegram_id = $1`

	settings, _ := json.Marshal(s)

	_, err := db.Exec(query, telegram_id, settings)
	if err != nil {
		panic(err)
	}
}

func UpdateLastCheck(telegram_id string, timestamp string) {
	query := `
        UPDATE ` + config[table_name] + `
        SET last_check = $2
        WHERE telegram_id = $1`

	_, err := db.Exec(query, telegram_id, timestamp)
	if err != nil {
		panic(err)
	}
}

func (s *Settings) Scan(src interface{}) error {
	strValue, ok := src.([]uint8)

	if !ok {
		return fmt.Errorf("settings field must be []uint8, got %T instead", src)
	}

	return json.Unmarshal([]byte(strValue), s)
}

func dbConfig() map[string]string {
	err := godotenv.Load("/root/rem-alert-api/.env")
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	conf := make(map[string]string)

	conf[db_host] = os.Getenv(db_host)
	conf[db_port] = os.Getenv(db_port)
	conf[db_user] = os.Getenv(db_user)
	conf[db_pass] = os.Getenv(db_pass)
	conf[db_name] = os.Getenv(db_name)
	conf[table_name] = os.Getenv(table_name)

	return conf
}
