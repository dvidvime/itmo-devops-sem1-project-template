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

type filterParams struct {
	startDate string
	endDate   string
	minPrice  int
	maxPrice  int
}

func setDefaultFilterParams() filterParams {
	return filterParams{
		startDate: "1970-01-01",
		endDate:   "5999-01-01",
		minPrice:  0,
		maxPrice:  2147483647,
	}
}

func roundFloat(val float64, precision uint) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

func createCsvFile(items []Item) ([][]string, error) {
	numItems := len(items)
	result := make([][]string, numItems+1)

	// heading line
	result[0] = []string{"id", "name", "category", "price", "create_date"}

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

func readCsvFile(csvFile io.Reader) ([]Item, int, error) {
	csvReader := csv.NewReader(csvFile)
	records, err := csvReader.ReadAll()
	if err != nil {
		log.Println(err)
		return nil, 0, err
	}

	const (
		Id        = 0
		Name      = 1
		Category  = 2
		Price     = 3
		CreatedAt = 4
	)

	var items []Item
	var incorrectRecords int

	for _, record := range records[1:] {
		id, err := strconv.Atoi(record[Id])
		if err != nil {
			log.Printf("Not a number : id %v : %v\n", record[Id], err)
			incorrectRecords++
			continue
		}
		name := record[Name]
		if len(name) == 0 {
			log.Printf("Empty name : id %v : %v\n", record[Id], err)
			incorrectRecords++
			continue
		}
		category := record[Category]
		if len(category) == 0 {
			log.Printf("Empty category : id %v : %v\n", record[Id], err)
			incorrectRecords++
			continue
		}
		price, err := strconv.ParseFloat(record[Price], 64)
		if price <= 0 || err != nil {
			log.Printf("Invalid price %v : id %v : %v\n", record[Price], record[Id], err)
			incorrectRecords++
			continue
		}
		createdAt, err := time.Parse("2006-01-02", record[CreatedAt])
		if err != nil {
			log.Printf("Invalid date %v : id %v : %v\n", record[CreatedAt], record[Id], err)
			incorrectRecords++
			continue
		}
		if createdAt.After(time.Now()) {
			log.Printf("Future date %v : id %v : %v\n", record[CreatedAt], record[Id], err)
			incorrectRecords++
			continue
		}

		items = append(items, Item{id, name, category, price, createdAt})
	}
	lenRead := len(records) - 1
	log.Printf("Read %d records, %d incorrect\n", lenRead, incorrectRecords)
	return items, lenRead, nil
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
		"INSERT INTO prices (id, name, category, price, create_date) VALUES($1,$2,$3,$4,$5)")
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

func readAllItems(ctx context.Context) ([]Item, error) {
	return readItems(ctx, "SELECT * from prices")
}

func readFilteredItems(ctx context.Context, p filterParams) ([]Item, error) {
	return readItems(ctx, "SELECT * from prices WHERE create_date between $1 and $2 AND price >= $3 and price <= $4",
		p.startDate, p.endDate, p.minPrice, p.maxPrice)
}

func readItems(ctx context.Context, query string, args ...any) ([]Item, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := DB.PingContext(ctx); err != nil {
		return nil, err
	}

	var items []Item

	rows, err := DB.QueryContext(ctx, query, args...)
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

func removeDuplicates(left []Item, right []Item) ([]Item, int) {
	// remove from left elements found in right and count duplicates
	rMap := make(map[int]struct{})
	for _, b := range right {
		rMap[b.Id] = struct{}{}
	}

	var uniqueItems []Item
	for _, a := range left {
		if _, found := rMap[a.Id]; !found {
			uniqueItems = append(uniqueItems, a)
		}
	}

	d := len(left) - len(uniqueItems)
	log.Printf("Removed %d duplicates\n", d)

	return uniqueItems, d
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
	var itemsRead int
	var totalCategories int
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
		if filepath.Ext(name) != ".csv" {
			continue
		}

		f := bytes.NewReader(content)

		items, itemsRead, err = readCsvFile(f)
		if err != nil {
			log.Println(err)
			http.Error(w, "Error reading csv", http.StatusInternalServerError)
			return
		}

		break
	}

	// get everything from database
	itemsDb, err := readAllItems(r.Context())
	if err != nil {
		log.Println(err)
		http.Error(w, "Error reading database", http.StatusInternalServerError)
		return
	}

	items, duplicatesCount := removeDuplicates(items, itemsDb)

	// write to database
	if err = saveItems(r.Context(), items); err != nil {
		log.Println(err)
		http.Error(w, "Error writing to database", http.StatusBadRequest)
		return
	}

	// retrieve total price
	row := DB.QueryRowContext(r.Context(), "SELECT SUM(price) FROM prices")
	if err = row.Scan(&totalPrice); err != nil {
		log.Println("Failed to retrieve total price", err)
	}

	// retrieve total categories
	row = DB.QueryRowContext(r.Context(), "SELECT COUNT(DISTINCT category) FROM prices")
	if err = row.Scan(&totalCategories); err != nil {
		log.Println("Failed to retrieve total categories", err)
	}

	// return JSON with summary
	resultSummary := struct {
		TotalCount      int     `json:"total_count"`
		DuplicatesCount int     `json:"duplicates_count"`
		TotalItems      int     `json:"total_items"`
		TotalCategories int     `json:"total_categories"`
		TotalPrice      float64 `json:"total_price"`
	}{
		TotalCount:      itemsRead,
		DuplicatesCount: duplicatesCount,
		TotalItems:      len(items),
		TotalCategories: totalCategories,
		TotalPrice:      roundFloat(totalPrice, 2),
	}

	log.Printf("%+v\n", resultSummary)

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

	params := setDefaultFilterParams()

	formStart := r.FormValue("start")
	formEnd := r.FormValue("end")
	formMin := r.FormValue("min")
	formMax := r.FormValue("max")

	// validate input
	_, err := time.Parse("2006-01-02", formStart)
	if err == nil {
		params.startDate = formStart
	}

	_, err = time.Parse("2006-01-02", formEnd)
	if err == nil {
		params.endDate = formEnd
	}

	formMinInt, err := strconv.Atoi(formMin)
	if err != nil && formMinInt > 0 {
		params.minPrice = formMinInt
	}

	formMaxInt, err := strconv.Atoi(formMax)
	if err != nil && formMaxInt > 0 {
		params.maxPrice = formMaxInt
	}

	items, err = readFilteredItems(r.Context(), params)
	if err != nil {
		log.Println(err)
		http.Error(w, "Error reading items", http.StatusInternalServerError)
	}

	// get everything from database
	/*items, err := readAllItems(r.Context())
	if err != nil {
		log.Println(err)
		http.Error(w, "Error reading items", http.StatusInternalServerError)
	}*/

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
