package main

// тут вы пишете код
// обращаю ваше внимание - в этом задании запрещены глобальные переменные

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"sync"

	_ "github.com/go-sql-driver/mysql"
)

type Handler struct {
	DB *sql.DB
}

type Item struct {
	Id          int
	Title       string
	Description string
	Updated     sql.NullString
}

type Table struct {
	Name string
	mu   sync.Mutex
}

//NewDbExplorer lists rows
func NewDbExplorer(db *sql.DB) (*http.ServeMux, error) {
	h := &Handler{DB: db}
	handler := h.ListTables
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	return mux, nil
}

func (h *Handler) ListTables(w http.ResponseWriter, r *http.Request) {
	items := []*Table{}

	rows, err := h.DB.Query("SHOW TABLES")
	if err != nil {
		log.Fatal(err)
	}

	for rows.Next() {
		it := &Table{}
		err := rows.Scan(&it.Name)
		if err != nil {
			panic(err)
		}
		items = append(items, it)
	}

	rows.Close()
	res, _ := json.Marshal(items)
	w.Header().Set("Content-Type", "application/json")
	w.Write(res)

}
