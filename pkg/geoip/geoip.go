package geoip

import (
	"embed"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"

	maxminddb "github.com/oschwald/maxminddb-golang"
)

const envGeoIPDB = "SANTAIZI_GEOIP_DB"

//go:embed geoip.db
var geoDBFS embed.FS

var (
	mu     sync.RWMutex
	db     *maxminddb.Reader
	dbData []byte
)

type IPInfo struct {
	Country       string `maxminddb:"country"`
	CountryName   string `maxminddb:"country_name"`
	Continent     string `maxminddb:"continent"`
	ContinentName string `maxminddb:"continent_name"`
	ASN           string `maxminddb:"asn"`
	ASName        string `maxminddb:"as_name"`
}

type HopInfo struct {
	CountryCode string
	CountryName string
	ASName      string
	Private     bool
}

func init() {
	loadDatabase()
}

func loadDatabase() {
	mu.Lock()
	defer mu.Unlock()
	if db != nil {
		_ = db.Close()
		db = nil
	}
	dbData = nil

	path := strings.TrimSpace(os.Getenv(envGeoIPDB))
	if path != "" {
		opened, err := maxminddb.Open(path) // #nosec G304 -- operator-configured GeoIP path
		if err == nil {
			db = opened
			log.Printf("SANTAIZI>> GeoIP 已从环境变量 %s 加载", envGeoIPDB)
			return
		}
		log.Printf("SANTAIZI>> 环境变量 %s 指向的库无法打开，回退内嵌库", envGeoIPDB)
	}

	data, err := geoDBFS.ReadFile("geoip.db")
	if err != nil {
		log.Printf("SANTAIZI>> 读取内嵌 GeoIP 库失败: %v", err)
		return
	}
	opened, err := maxminddb.FromBytes(data)
	if err != nil {
		log.Printf("SANTAIZI>> 内嵌 GeoIP 库不可用（源码构建是占位；Release 会拉取 country.mmdb）: %v", err)
		return
	}
	db = opened
	dbData = data
}

func Lookup(ip net.IP, record *IPInfo) (string, error) {
	if ip == nil {
		return "", fmt.Errorf("IP not found")
	}
	if record == nil {
		record = &IPInfo{}
	}
	mu.RLock()
	defer mu.RUnlock()
	if db == nil {
		return "", fmt.Errorf("IP not found")
	}
	if err := db.Lookup(ip, record); err != nil {
		return "", err
	}
	if record.Country != "" {
		return strings.ToLower(record.Country), nil
	}
	if record.Continent != "" {
		return strings.ToLower(record.Continent), nil
	}
	return "", fmt.Errorf("IP not found")
}

// LookupCodeFromAddr resolves a Host.IP bundle (`ipv4`, `ipv6`, or `ipv4/ipv6`).
// Dual-stack prefers IPv4, matching the agent default (UseIPv6CountryCode=false).
func LookupCodeFromAddr(v4v6Bundle string) string {
	ip := queryIPFromBundle(v4v6Bundle)
	if ip == nil {
		return ""
	}
	code, err := Lookup(ip, &IPInfo{})
	if err != nil {
		return ""
	}
	return code
}

func LookupHop(addr string) HopInfo {
	ip := net.ParseIP(strings.TrimSpace(addr))
	if ip == nil {
		return HopInfo{}
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return HopInfo{Private: true}
	}
	record := &IPInfo{}
	code, err := Lookup(ip, record)
	if err != nil {
		return HopInfo{}
	}
	return HopInfo{
		CountryCode: code,
		CountryName: strings.TrimSpace(record.CountryName),
		ASName:      strings.TrimSpace(record.ASName),
	}
}

func FormatHopGeo(info HopInfo) string {
	if info.Private {
		return ""
	}
	country := strings.TrimSpace(info.CountryName)
	if country == "" {
		country = strings.ToUpper(strings.TrimSpace(info.CountryCode))
	}
	asName := strings.TrimSpace(info.ASName)
	switch {
	case country != "" && asName != "":
		return country + " · " + asName
	case asName != "":
		return asName
	default:
		return country
	}
}

func queryIPFromBundle(v4v6Bundle string) net.IP {
	parts := strings.Split(strings.TrimSpace(v4v6Bundle), "/")
	var fallback net.IP
	for _, part := range parts {
		parsed := net.ParseIP(strings.TrimSpace(part))
		if parsed == nil {
			continue
		}
		if parsed.To4() != nil {
			return parsed
		}
		if fallback == nil {
			fallback = parsed
		}
	}
	return fallback
}
