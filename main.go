package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"
)

// Broker hanterar klientanslutningar och meddelanden
type Broker struct {
	// Mapp över anslutna klienter
	clients map[chan string]bool
	// Mutex för att säkra tråd-säkerhet
	mutex         sync.Mutex
	clientChanges chan int // Ny kanal för klientändringar
}

// Skapar en ny Broker-instans
func NewBroker() *Broker {
	return &Broker{
		clients:       make(map[chan string]bool),
		clientChanges: make(chan int),
	}
}

// ServeHTTP hanterar inkommande SSE-anslutningar
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Println("Ny klient ansluten")

	// Ställ in rätt headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Skapa en kanal för denna klient
	messageChan := make(chan string)

	// Lägg till klienten till listan
	b.mutex.Lock()
	b.clients[messageChan] = true
	clientCount := len(b.clients)
	b.mutex.Unlock()

	b.clientChanges <- clientCount

	// Ta bort klienten när funktionen avslutas
	defer func() {
		b.mutex.Lock()
		delete(b.clients, messageChan)
		b.mutex.Unlock()
		close(messageChan)
		log.Println("Klient frånkopplad")

		b.clientChanges <- clientCount
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Din webbläsare stöder inte SSE", http.StatusInternalServerError)
		return
	}

	// Lyssnar på meddelanden för denna anslutning
	for {
		select {
		case msg, ok := <-messageChan:
			if !ok {
				log.Println("Meddelandekanalen stängd")
				return
			}
			// Skicka meddelandet till klienten
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			// Klienten har stängt anslutningen
			log.Println("Klienten kopplade från")
			return
		}
	}
}

// Broadcast skickar ett meddelande till alla anslutna klienter
func (b *Broker) Broadcast(message string) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	for clientChan := range b.clients {
		select {
		case clientChan <- message:
			// Meddelande skickat
		default:
			// Om klienten inte kan ta emot, ta bort den
			close(clientChan)
			delete(b.clients, clientChan)
		}
	}
}

func (b *Broker) Start() {
	go func() {
		for {
			select {
			case clientCount := <-b.clientChanges:
				// Skapa ett meddelande med antalet klienter
				message := fmt.Sprintf("clients: %d", clientCount)
				b.Broadcast(message)
			}
		}
	}()
}

func main() {
	broker := NewBroker()
	broker.Start() // Starta hanteringen av klientändringar
	// SSE Endpoint
	http.Handle("/sse", broker)

	// Endpoint för att skicka nya meddelanden
	http.HandleFunc("/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Endast POST-metoden stöds", http.StatusMethodNotAllowed)
			return
		}
		// Hämta meddelandet från POST-data
		message := r.FormValue("message")
		if message == "" {
			http.Error(w, "Meddelandet kan inte vara tomt", http.StatusBadRequest)
			return
		}
		// Broadcasta meddelandet till alla klienter
		broker.Broadcast(message)
		fmt.Fprintf(w, "Meddelandet '%s' skickades till %d klient(er)\n", message, len(broker.clients))
		log.Printf("Meddelandet '%s' skickades till %d klient(er)", message, len(broker.clients))
	})

	// Test-endpoint för att verifiera serverfunktion
	http.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Servern fungerar!")
	})

	// Statisk filserver för frontend
	http.Handle("/", http.FileServer(http.Dir("./public")))
	// http.Handle("/public/", http.StripPrefix("/public/", http.FileServer(http.Dir("./public"))))

	fmt.Println("Servern körs på http://localhost:8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
