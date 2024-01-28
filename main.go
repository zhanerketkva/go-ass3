package main

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

// Product structure represents a product in the store
type Product struct {
	ID    int
	Name  string
	Size  string
	Price float64
}

var (
	db      *sql.DB
	log     *logrus.Logger
	limiter = rate.NewLimiter(1, 3) // Rate limit of 1 request per second with a burst of 3 requests
)

func initDB() *sql.DB {
	connStr := "user=postgres password=Medina+15 dbname=go sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Error opening database connection:", err)
		panic(err)
	}

	err = db.Ping()
	if err != nil {
		log.Fatal("Error connecting to the database:", err)
		panic(err)
	}

	log.Info("Connected to the database")

	return db
}

func fetchProductsFromDB(filter, sortBy string, page, pageSize int) ([]Product, error) {
	var products []Product

	var query string
	var args []interface{}

	if filter != "" {
		query = "SELECT id, name, size, price FROM products WHERE name ILIKE $1"
		args = append(args, "%"+filter+"%")
	} else {
		query = "SELECT id, name, size, price FROM products"
	}

	if sortBy != "" {
		if sortBy == "size" {
			query += " ORDER BY CASE size " +
				"WHEN 'xs' THEN 1 " +
				"WHEN 's' THEN 2 " +
				"WHEN 'm' THEN 3 " +
				"WHEN 'l' THEN 4 " +
				"WHEN 'xl' THEN 5 " +
				"WHEN 'xxl' THEN 6 " +
				"ELSE 7 " +
				"END"
		} else {
			query += " ORDER BY " + sortBy
		}
	}

	query += " LIMIT $1 OFFSET $2"
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := db.Query(query, args...)
	if err != nil {
		log.Error("Error fetching products from the database:", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Size, &p.Price); err != nil {
			log.Error("Error scanning product row:", err)
			continue
		}
		products = append(products, p)
	}

	if err := rows.Err(); err != nil {
		log.Error("Error iterating over product rows:", err)
		return nil, err
	}

	return products, nil
}

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("filter")
	sortBy := r.URL.Query().Get("sort")

	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}

	pageSize, err := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if err != nil || pageSize < 1 {
		pageSize = 10
	}

	// Rate limiting check
	if !limiter.Allow() {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	products, err := fetchProductsFromDB(filter, sortBy, page, pageSize)
	if err != nil {
		log.Error("Error fetching products from the database:", err)
		http.Error(w, "Error fetching products from the database", http.StatusInternalServerError)
		return
	}

	tmpl, err := template.New("index").Parse(`
    <!DOCTYPE html>
    <html>
    <head>
        <title>Online Store</title>
        <style>
        body {
            font-family: 'Arial', sans-serif;
            margin: 20px;
        }
        h1 {
            color: #333;
        }
        form {
            margin-bottom: 20px;
        }
        table {
            width: 100%;
            border-collapse: collapse;
            margin-top: 20px;
        }
        th, td {
            border: 1px solid #ddd;
            padding: 8px;
            text-align: left;
        }
        th {
            background-color: #f2f2f2;
        }
        td {
            color: #333;
        }
        button {
            background-color: #4CAF50;
            color: white;
            padding: 8px 12px;
            border: none;
            border-radius: 4px;
            cursor: pointer;
        }
        button:hover {
            background-color: #45a049;
        }
        a {
            text-decoration: none;
            color: #337ab7;
        }
        div.pagination {
            margin-top: 20px;
        }
        span.page {
            margin: 0 10px;
        }
    </style>
    </head>
    <body>
        <h1>Welcome to the Online Store!</h1>
        <form action="/" method="get">
            <label for="filter">Filter:</label>
            <input type="text" id="filter" name="filter" placeholder="Enter filter" value="{{.Filter}}">
            <button type="submit">Apply Filter</button>
        </form>
        <form action="/" method="get">
            <input type="hidden" name="filter" value="{{.Filter}}">
            <label for="sort">Sort by:</label>
            <select name="sort" id="sort">
                <option value="">Default</option>
                <option value="size" {{if eq .SortBy "size"}}selected{{end}}>Size</option>
                <option value="price" {{if eq .SortBy "price"}}selected{{end}}>Price</option>
            </select>
            <button type="submit">Apply Sort</button>
        </form>
        <h2>Products:</h2>
        <table border="1">
            <tr>
                <th>ID</th>
                <th>Name</th>
                <th>Size</th>
                <th>Price</th>
                <th>Action</th>
            </tr>
            {{range .Products}}
                <tr>
                    <td>{{.ID}}</td>
                    <td>{{.Name}}</td>
                    <td>{{.Size}}</td>
                    <td>${{.Price}}</td>
                    <td>
                        <form method="post" action="/delete/{{.ID}}">
                            <input type="hidden" name="_method" value="DELETE">
                            <button type="submit">Delete</button>
                        </form>
                        <form method="get" action="/edit/{{.ID}}">
                            <button type="submit">Edit</button>
                        </form>
                    </td>
                </tr>
            {{end}}
        </table>
        <a href="/add-product">Add Product</a>
        <div>
        <span>Page: {{.Page}}</span>
        <a href="?page={{.PrevPage}}&pageSize={{.PageSize}}">Previous</a>
        <a href="?page={{.NextPage}}&pageSize={{.PageSize}}">Next</a>
    </div>
    </body>
    </html>
    `)

	if err != nil {
		log.Error("Error parsing template:", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Filter   string
		SortBy   string
		Products []Product
		Page     int
		PrevPage int
		NextPage int
		PageSize int
	}{
		Filter:   filter,
		SortBy:   sortBy,
		Products: products,
		Page:     page,
		PrevPage: page - 1,
		NextPage: page + 1,
		PageSize: pageSize,
	}

	tmpl.Execute(w, data)
}

func DeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not supported", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Path[len("/delete/"):]
	productID, err := strconv.Atoi(id)
	if err != nil {
		log.Error("Invalid product ID:", err)
		http.Error(w, "Invalid product ID", http.StatusBadRequest)
		return
	}

	_, err = db.Exec("DELETE FROM products WHERE id = $1", productID)
	if err != nil {
		log.Error("Error deleting from database:", err)
		http.Error(w, "Error deleting from database", http.StatusInternalServerError)
		return
	}

	log.Printf("Product deleted with ID: %d\n", productID)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func AddProductHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.New("addProduct").Parse(`
		<!DOCTYPE html>
		<html>
		<head>
			<title>Add Product</title>
		</head>
		<body>
			<h1>Add a new product</h1>
			<form method="post" action="/add-product-post">
				<label for="name">Name:</label>
				<input type="text" name="name" required><br>
				<label for="size">Size:</label>
				<input type="text" name="size" required><br>
				<label for="price">Price:</label>
				<input type="number" name="price" step="0.01" required><br>
				<input type="submit" value="Add Product">
			</form>
			<a href="/">Back to Home</a>
		</body>
		</html>
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tmpl.Execute(w, nil)
}

func AddProductPostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not supported", http.StatusMethodNotAllowed)
		return
	}

	_, err := db.Exec("INSERT INTO products (name, size, price) VALUES ($1, $2, $3)",
		r.FormValue("name"), r.FormValue("size"), r.FormValue("price"))
	if err != nil {
		fmt.Println("Error inserting into database:", err)
		http.Error(w, "Error inserting into database", http.StatusInternalServerError)
		return
	}

	fmt.Printf("New product added: Name=%s, Size=%s, Price=%s\n", r.FormValue("name"), r.FormValue("size"), r.FormValue("price"))

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func EditProductHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/edit/"):]
	productID, err := strconv.Atoi(id)
	if err != nil {
		http.Error(w, "Invalid product ID", http.StatusBadRequest)
		return
	}

	var product Product
	err = db.QueryRow("SELECT id, name, size, price FROM products WHERE id = $1", productID).
		Scan(&product.ID, &product.Name, &product.Size, &product.Price)
	if err != nil {
		fmt.Println("Error fetching product details:", err)
		http.Error(w, "Error fetching product details", http.StatusInternalServerError)
		return
	}

	tmpl, err := template.New("editProduct").Parse(`
        <!DOCTYPE html>
        <html>
        <head>
            <title>Edit Product</title>
        </head>
        <body>
            <h1>Edit Product</h1>
            <form method="post" action="/edit-product-post/{{.ID}}">
                <label for="name">Name:</label>
                <input type="text" name="name" value="{{.Name}}" required><br>
                <label for="size">Size:</label>
                <input type="text" name="size" value="{{.Size}}" required><br>
                <label for="price">Price:</label>
                <input type="number" name="price" step="0.01" value="{{.Price}}" required><br>
                <input type="submit" value="Save Changes">
            </form>
            <a href="/">Back to Home</a>
        </body>
        </html>
    `)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tmpl.Execute(w, product)
}

func EditProductPostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not supported", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Path[len("/edit-product-post/"):]
	productID, err := strconv.Atoi(id)
	if err != nil {
		http.Error(w, "Invalid product ID", http.StatusBadRequest)
		return
	}

	_, err = db.Exec("UPDATE products SET name=$1, size=$2, price=$3 WHERE id=$4",
		r.FormValue("name"), r.FormValue("size"), r.FormValue("price"), productID)
	if err != nil {
		fmt.Println("Error updating product in database:", err)
		http.Error(w, "Error updating product in database", http.StatusInternalServerError)
		return
	}

	fmt.Printf("Product updated with ID: %d\n", productID)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func main() {
	// Initialize logger
	log = logrus.New()
	log.SetFormatter(&logrus.JSONFormatter{})

	// Initialize database
	db = initDB()
	defer db.Close()

	// Set up HTTP server
	server := &http.Server{
		Addr:    ":8080",
		Handler: nil, // Your handler will be set later
	}

	// Set up routes
	http.HandleFunc("/", IndexHandler)
	http.HandleFunc("/delete/", DeleteHandler)
	http.HandleFunc("/add-product", AddProductHandler)
	http.HandleFunc("/add-product-post", AddProductPostHandler)
	http.HandleFunc("/edit/", EditProductHandler)
	http.HandleFunc("/edit-product-post/", EditProductPostHandler)

	// Run server in a goroutine for graceful shutdown
	go func() {
		log.Println("Server is running at http://localhost:8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Server error:", err)
		}
	}()

	// Handle graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Server is shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatal("Server shutdown error:", err)
	}

	log.Info("Server has stopped")
}
