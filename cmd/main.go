package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
)

var euroToGBP float64

type Product struct {
	URL           string  `json:"url"`
	FromPrice     string  `json:"fromPrice"`
	Name          string  `json:"name,omitempty"`
	ImageURL      string  `json:"imageURL,omitempty"`
	PriceTrend    string  `json:"priceTrend,omitempty"`
	FromPriceVal  float64 `json:"-"`
	PriceTrendVal float64 `json:"priceTrendVal,omitempty"`
	ChangePercent float64 `json:"changePercent,omitempty"`
}

func init() {
	euroToGBP = getEuroToGBP()
}

func main() {
	// Use paths relative to the project root
	productsPath := filepath.Join("data", "products.json")
	outputPath := filepath.Join("docs", "output.json")

	file, err := os.Open(productsPath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// Map of category -> slice of Products
	productsMap := make(map[string][]Product)
	if err := json.NewDecoder(file).Decode(&productsMap); err != nil {
		log.Fatal(err)
	}

	// Scrape products for each category
	for category, products := range productsMap {
		for i := range products {
			products[i].FromPriceVal = parsePrice(products[i].FromPrice)
			scrapeProductData(&products[i])
		}

		// Sort each category's products by ChangePercent desc
		sort.Slice(products, func(i, j int) bool {
			return products[i].ChangePercent > products[j].ChangePercent
		})

		productsMap[category] = products
	}

	// Save updated data to output.json
	outFile, err := os.Create(outputPath)
	if err != nil {
		log.Fatal(err)
	}
	defer outFile.Close()

	encoder := json.NewEncoder(outFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(productsMap); err != nil {
		log.Fatal(err)
	}

	fmt.Println("Scraping complete, data saved to output.json")
}

func getEuroToGBP() float64 {
	resp, err := http.Get("https://api.frankfurter.dev/v1/latest?symbols=GBP")
	if err != nil {
		log.Println("Failed to fetch exchange rate:", err)
		return 0.85 // fallback
	}
	defer resp.Body.Close()

	var result struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Println("Failed to decode exchange rate:", err)
		return 0.85 // fallback
	}

	rate, ok := result.Rates["GBP"]
	if !ok {
		log.Println("GBP rate not found in response")
		return 0.85
	}

	return rate
}

func parsePrice(price string) float64 {
	price = strings.ReplaceAll(price, "€", "")
	price = strings.ReplaceAll(price, "£", "")
	price = strings.TrimSpace(price)

	// If the string has both '.' and ',' and the last ',' comes after the last '.', assume European style
	if strings.Contains(price, ",") && strings.Contains(price, ".") {
		if strings.LastIndex(price, ",") > strings.LastIndex(price, ".") {
			// Example: "1.046,50" → "1046.50"
			price = strings.ReplaceAll(price, ".", "")
			price = strings.ReplaceAll(price, ",", ".")
		} else {
			// Example: "1,046.50" → "1046.50"
			price = strings.ReplaceAll(price, ",", "")
		}
	} else if strings.Contains(price, ",") {
		// Likely just decimal comma: "445,62" → "445.62"
		price = strings.ReplaceAll(price, ",", ".")
	} else if strings.Count(price, ".") > 1 {
		// Handle broken US format: "1.046.50" → "1046.50"
		lastDot := strings.LastIndex(price, ".")
		price = strings.ReplaceAll(price[:lastDot], ".", "") + price[lastDot:]
	}

	val, err := strconv.ParseFloat(price, 64)
	if err != nil {
		log.Printf("Failed to parse price: %s (%v)", price, err)
		return 0.0
	}
	return val
}

func scrapeProductData(p *Product) {
	html, err := fetchWithChrome(p.URL)
	if err != nil {
		log.Printf("Chrome fetch failed for %s: %v", p.URL, err)
		return
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		log.Printf("Failed to parse HTML from %s", p.URL)
		return
	}

	title := doc.Find("title").First().Text()
	if strings.Contains(strings.ToLower(title), "just a moment") {
		log.Printf("Page blocked or not loaded correctly for URL: %s", p.URL)
		p.Name = "Blocked or loading issue"
		return
	}
	p.Name = strings.TrimSuffix(title, " | Cardmarket")

	doc.Find("dt").Each(func(i int, s *goquery.Selection) {
		if strings.TrimSpace(s.Text()) == "Price Trend" {
			eur := strings.TrimSpace(s.Next().Find("span").Text())
			p.PriceTrend = eur
			p.PriceTrendVal = parsePrice(eur) * euroToGBP
			if p.FromPriceVal > 0 {
				p.ChangePercent = ((p.PriceTrendVal - p.FromPriceVal) / p.FromPriceVal) * 100
			}
		}
	})

	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		src, _ := s.Attr("src")
		alt, _ := s.Attr("alt")
		if strings.Contains(alt, "Elite Trainer Box") {
			p.ImageURL = src
		}
	})
}

func fetchWithChrome(url string) (string, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-software-rasterizer", true),
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var html string
	tasks := chromedp.Tasks{
		chromedp.Navigate(url),
		chromedp.Sleep(5 * time.Second),
		chromedp.OuterHTML("html", &html),
	}

	if err := chromedp.Run(ctx, tasks); err != nil {
		return "", err
	}
	return html, nil
}
