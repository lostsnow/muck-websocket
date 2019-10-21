package main

import (
	"bufio"
	"bytes"
	"flag"
	"log"
	"io/ioutil"
	"net/http"
	"sync"

	"github.com/tehbilly/gmudc/telnet"
	"github.com/gorilla/websocket"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// Flags
var addr = flag.String("addr", "localhost:8000", "http service address")
var muckHost = flag.String("muck", "localhost:4021",
	"host and port for proxied muck")
var isGBK = flag.Bool("gbk", false,
	"is muck charset GBK.")

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func openTelnet() (*telnet.Connection, error) {
	conn := telnet.New()

	err := conn.Dial("tcp", *muckHost)

	return conn, err
}

func telnetProxy(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "Not found", 404)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "Error creating websocket", 500)
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()

	log.Printf("Opening a proxy for '%s'", r.RemoteAddr)
	t, err := openTelnet()
	if err != nil {
		log.Println("Error opening telnet proxy: ", err)
		return
	}
	defer t.Close()

	log.Printf("Connection open for '%s'. Proxying.", r.RemoteAddr)

	var wg sync.WaitGroup
	var once sync.Once
	wg.Add(1) // Exit when either goroutine stops.

	// Send messages from the websocket to the MUCK.
	go func() {
		defer once.Do(func() { wg.Done() })
		for {
			_, bs, err := c.ReadMessage()
			if err != nil {
				log.Printf("Error reading from ws(%s): %v", r.RemoteAddr, err)
				break
			}

			if *isGBK == true {
				bs, err = utf8ToGbk(bs)
				if err != nil {
					log.Printf("Error convert encoding")
					break
				}
			}

			// TODO: Partial writes.
			if _, err := t.Write(bs); err != nil {
				log.Printf("Error sending message to Muck for %s: %v",
					r.RemoteAddr, err)
				break
			}
		}
	}()

	// Send messages from the MUCK to the websocket.
	go func() {
		defer once.Do(func() { wg.Done() })
		br := bufio.NewReader(t)
		for {
			bs := make([]byte, 1024)
			if _, err := br.Read(bs); err != nil {
				log.Printf("Error reading from muck for %s: %v", r.Host, err)
				break
			}

			if *isGBK == true {
				bs, err = gbkToUtf8(bs)
				if err != nil {
					log.Printf("Error convert encoding")
					break
				}
			}

			if err = c.WriteMessage(websocket.TextMessage, bs); err != nil {
				log.Printf("Error sending to ws(%s): %v", r.RemoteAddr, err)
				break
			}
		}
	}()

	// Wait until either go routine exits and then close both connections.
	wg.Wait()
	log.Printf("Proxying completed for %s", r.RemoteAddr)
}

func gbkToUtf8(s []byte) ([]byte, error) {
    reader := transform.NewReader(bytes.NewReader(s), simplifiedchinese.GBK.NewDecoder())
    return ioutil.ReadAll(reader)
}

func utf8ToGbk(s []byte) ([]byte, error) {
	reader := transform.NewReader(bytes.NewReader(s), simplifiedchinese.GBK.NewEncoder())
	return ioutil.ReadAll(reader)
}

func main() {
	flag.Parse()
	log.SetFlags(0)

	http.HandleFunc("/", telnetProxy)
	err := http.ListenAndServe(*addr, nil)
	// Use this instead if you want to do SSL. You'll need to use `openssl`
	// to generate "cert.pem" and "key.pem" files.
	// err := http.ListenAndServeTLS(*addr, "cert.pem", "key.pem", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
