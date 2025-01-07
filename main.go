package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"io"
	"log"
	"math"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	DB_HOST = "localhost"
	DB_PORT = 5432
	DB_USER = "validator"
	DB_PASS = "val1dat0r"
	DB_NAME = "project-sem-1"
)

var DB *sql.DB

type Item struct {
	Id        int
	Name      string
	Category  string
	Price     float64
	CreatedAt time.Time
}

func roundFloat(val float64, precision uint) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

func createCsvFile(items []Item) ([][]string, error) {
	numItems := len(items)
	result := make([][]string, numItems+1)

	// heading line
	result[0] = []string{"id", "name", "category", "price", "created"}

	// add rows with formatted data
	for i := 0; i < numItems; i++ {
		result[i+1] = []string{
			strconv.Itoa(items[i].Id),
			items[i].Name,
			items[i].Category,
			fmt.Sprintf("%.2f", items[i].Price),
			fmt.Sprintf("%s", items[i].CreatedAt.Format("2006-01-02")),
		}
	}

	return result, nil
}

func readCsvFile(csvFile io.Reader) ([]Item, int, float64, error) {
	csvReader := csv.NewReader(csvFile)
	records, err := csvReader.ReadAll()
	if err != nil {
		log.Println(err)
		return nil, 0, 0, err
	}

	const (
		Id        = 0
		Name      = 1
		Category  = 2
		Price     = 3
		CreatedAt = 4
	)

	var items []Item
	var categories []string
	var priceSum float64

	for _, record := range records[1:] {
		id, _ := strconv.Atoi(record[Id])
		name := record[Name]
		category := record[Category]
		price, _ := strconv.ParseFloat(record[Price], 64)
		createdAt, _ := time.Parse("2006-01-02", record[CreatedAt])

		if !slices.Contains(categories, category) {
			categories = append(categories, category)
		}

		priceSum += price

		items = append(items, Item{id, name, category, price, createdAt})

		//fmt.Println(id, name, category, price, createdAt)
	}
	return items, len(categories), roundFloat(priceSum, 2), nil
}

func saveItems(ctx context.Context, items []Item) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// check connection
	if err := DB.PingContext(ctx); err != nil {
		return err
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}

	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO prices (id, name, category, price, created) VALUES($1,$2,$3,$4,$5)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, i := range items {
		//fmt.Printf("%+v\n", i)
		_, err := stmt.ExecContext(ctx, i.Id, i.Name, i.Category, i.Price, i.CreatedAt)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func readItems(ctx context.Context) ([]Item, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := DB.PingContext(ctx); err != nil {
		return nil, err
	}

	var items []Item

	rows, err := DB.QueryContext(ctx, "SELECT * from prices")
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	for rows.Next() {
		var i Item
		err = rows.Scan(&i.Id, &i.Name, &i.Category, &i.Price, &i.CreatedAt)
		if err != nil {
			return nil, err
		}

		items = append(items, i)
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}
	return items, nil
}

func extractArchive(file multipart.File, fileHeader *multipart.FileHeader, paramType string) (map[string][]byte, error) {
	contents := make(map[string][]byte)

	fileType := strings.ReplaceAll(filepath.Ext(fileHeader.Filename), ".", "")

	log.Printf("Uploaded file extension: %v", fileType)

	if (paramType == "zip" || paramType == "") && fileType == "zip" {
		zipReader, err := zip.NewReader(file, fileHeader.Size)
		if err != nil {
			return nil, err
		}

		for _, zf := range zipReader.File {
			rc, err := zf.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			buf := new(bytes.Buffer)
			if _, err := io.Copy(buf, rc); err != nil {
				return nil, err
			}
			contents[zf.Name] = buf.Bytes()
		}

	} else if paramType == "tar" && fileType == paramType {
		tarReader := tar.NewReader(file)

		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}

			buf := new(bytes.Buffer)
			if _, err := io.Copy(buf, tarReader); err != nil {
				return nil, err
			}
			contents[header.Name] = buf.Bytes()
		}
	} else {
		return nil, fmt.Errorf("unsupported file type or type mismatch (expected %v, got %v)", paramType, fileType)
	}
	return contents, nil
}

func postHandler(w http.ResponseWriter, r *http.Request) {
	var items []Item
	var numCategories int
	var totalPrice float64

	paramType := r.FormValue("type")

	log.Printf("URL type parameter: %v", paramType)

	// read file
	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		log.Println(err)
		http.Error(w, "Error receiving file", http.StatusBadRequest)
		return
	}

	defer file.Close()

	// unpacking
	archiveContents, err := extractArchive(file, fileHeader, paramType)
	if err != nil {
		log.Println(err)
		http.Error(w, "Error unpacking archive", http.StatusBadRequest)
		return
	}

	// find and open data.csv from archive
	for name, content := range archiveContents {
		if filepath.Base(name) != "data.csv" {
			continue
		}

		f := bytes.NewReader(content)

		items, numCategories, totalPrice, err = readCsvFile(f)
		if err != nil {
			log.Println(err)
			http.Error(w, "Error reading csv", http.StatusInternalServerError)
			return
		}

		break
	}

	// write to database
	if err = saveItems(r.Context(), items); err != nil {
		log.Println(err)
		http.Error(w, "Error writing to database", http.StatusBadRequest)
		return
	}

	// return JSON with summary
	resultSummary := struct {
		TotalItems      int     `json:"total_items"`
		TotalCategories int     `json:"total_categories"`
		TotalPrice      float64 `json:"total_price"`
	}{
		TotalItems:      len(items),
		TotalCategories: numCategories,
		TotalPrice:      totalPrice,
	}

	jsonData, err := json.Marshal(resultSummary)
	if err != nil {
		log.Println(err)
		http.Error(w, "Error marshalling json", http.StatusInternalServerError)
	}

	w.Header().Set("Content-Type", "application/json")

	if _, err := w.Write(jsonData); err != nil {
		log.Println(err)
		http.Error(w, "Error writing response", http.StatusInternalServerError)
	}
}

func getHandler(w http.ResponseWriter, r *http.Request) {
	var items []Item

	// get everything from database
	items, err := readItems(r.Context())
	if err != nil {
		log.Println(err)
		http.Error(w, "Error reading items", http.StatusInternalServerError)
	}

	// prepare csv
	csvFile, err := createCsvFile(items)
	if err != nil {
		log.Println(err)
		http.Error(w, "Error preparing csv file", http.StatusInternalServerError)
	}

	// create archive
	zipWriter := zip.NewWriter(w)

	f, err := zipWriter.Create("data.csv")
	if err != nil {
		log.Println(err)
	}

	csvWriter := csv.NewWriter(f)

	if err := csvWriter.WriteAll(csvFile); err != nil {
		log.Println(err)
		http.Error(w, "Error writing response", http.StatusInternalServerError)
	}

	err = zipWriter.Close()
	if err != nil {
		log.Fatal(err)
	}

	/*csvWriter := csv.NewWriter(w)

	w.Header().Set("Content-Type", "text/csv")

	if err := csvWriter.WriteAll(csvFile); err != nil {
		log.Println(err)
		http.Error(w, "Error writing response", http.StatusInternalServerError)
	}*/

	/*w.Header().Set("Content-Type", "application/json")

	jsonData, err := json.Marshal(items)
	if err != nil {
		log.Println(err)
		http.Error(w, "Error marshalling json", http.StatusInternalServerError)
	}

	if _, err := w.Write(jsonData); err != nil {
		log.Println(err)
		http.Error(w, "Error writing response", http.StatusInternalServerError)
	}*/
}

func main() {
	ps := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		DB_HOST, DB_PORT, DB_USER, DB_PASS, DB_NAME)

	var err error

	DB, err = sql.Open("pgx", ps)
	if err != nil {
		panic(err)
	}
	defer DB.Close()

	router := http.NewServeMux()

	router.HandleFunc("POST /api/v0/prices", postHandler) // Handle the incoming file
	router.HandleFunc("GET /api/v0/prices", getHandler)   //Get items

	log.Fatal(http.ListenAndServe(":8080", router))
}
