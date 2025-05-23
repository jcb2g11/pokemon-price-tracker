# Pokemon Price Tracker

This project scrapes Cardmarket product price trends and converts currency from Euro to GBP.

## Structure

- `cmd/main.go` - Main scraper program
- `data/products.json` - Input product URLs and data
- `static/output.json` - Output scraped data
- `index.html` - Frontend for displaying data (if applicable)

## Usage

Build and run the Go program:

```bash
go run cmd/main.go
