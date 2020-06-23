package main

// тут вы пишете код
// обращаю ваше внимание - в этом задании запрещены глобальные переменные

import (
	"database/sql"
	"fmt"
	"net/http"

	_ "github.com/go-sql-driver/mysql"
)

// DbScanner сканирует структуру базы
type DbScanner struct {
	DB *sql.DB
}

// DbExplorer основной метод, который оперирует с базой данных
type DbExplorer struct {
	DB     *sql.DB
	tables []string
}

// Item пока не используем
type Item struct {
	ID          int
	Title       string
	Description string
	Updated     sql.NullString
}

// Table описывает таблицу в базе
type Table struct {
	Name string
	key  int
}

// ListTableNames перечисляет названия таблиц в базе
func (s *DbScanner) ListTableNames() (tableNames []string, err error) {
	rows, err := s.DB.Query("SHOW TABLES")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch table names: %s", err)
	}
	var n string
	for rows.Next() {
		err := rows.Scan(&n)
		if err != nil {
			panic(err)
		}
		tableNames = append(tableNames, n)
	}

	rows.Close()
	return tableNames, nil
}

// NewDbScanner создаёт новый экземпляр сканера базы
func NewDbScanner(db *sql.DB) *DbScanner {
	return &DbScanner{db}
}

// NewDbExplorer создёт новый экземпляр оператора базы данных
func NewDbExplorer(db *sql.DB) (*DbExplorer, error) {
	tables, err := NewDbScanner(db).ListTableNames()
	if err != nil {
		return nil, fmt.Errorf("failed to make new DbExplorer: %s", err)
	}
	return &DbExplorer{db, tables}, nil
}

// ServeHTTP основной обработчик http запросов
func (e *DbExplorer) ServeHTTP(w http.ResponseWriter, r *http.Request) {}

//NewDbExplorer lists rows
// func NewDbExplorer(db *sql.DB) (*http.ServeMux, error) {
// 	s := &DbScanner{DB: db}
// 	handler := s.ListTables
// 	mux := http.NewServeMux()
// 	mux.HandleFunc("/", handler)
// 	return mux, nil
// }

// func (d *DbExplorer) ListTables(w http.ResponseWriter, r *http.Request) {
// 	tableNames := []string{}

// 	switch r.URL.Path {
// 	case "/":
// 		break

// 	default:
// 		w.Header().Set("Content-Type", "application/json")
// 		w.Header().Set("Server", "Go!")
// 		w.WriteHeader(http.StatusNotFound)
// 		res := struct {
// 			Error string `json:"error"`
// 		}{}
// 		res.Error = "unknown table"
// 		jsonres, err := json.Marshal(res)
// 		if err != nil {
// 			panic(err)
// 		}
// 		w.Write(jsonres)
// 		return
// 	}

// 	rows, err := h.DB.Query("SHOW TABLES")
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	for rows.Next() {
// 		it := &Table{}
// 		err := rows.Scan(&it.Name)
// 		if err != nil {
// 			panic(err)
// 		}
// 		tableNames = append(tableNames, it.Name)
// 	}

// 	rows.Close()

// 	res := struct {
// 		Payload struct {
// 			Tables []string `json:"tables"`
// 		} `json:"response"`
// 	}{}
// 	res.Payload.Tables = tableNames

// 	jsonres, err := json.Marshal(res)

// 	if err != nil {
// 		http.Error(w, err.Error(), http.StatusInternalServerError)
// 		return
// 	}

// 	w.Header().Set("Content-Type", "application/json")
// 	w.Write(jsonres)
// }
