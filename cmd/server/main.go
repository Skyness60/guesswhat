package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

type Message struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type Player struct {
	Conn     *websocket.Conn
	Pseudo   string
	Score    int
	JoinedAt time.Time
}

type Room struct {
	Code        string
	Players     []*Player
	PlayersMu   sync.Mutex
	Host        *Player
	DrawPlayer  *Player
	CurrentWord string
	Guessed     map[*Player]bool
	Round       int
	Started     bool
}

var (
	rooms   = make(map[string]*Room)
	roomsMu sync.Mutex
	words   = []string{
		"chat", "chien", "maison", "voiture", "plage", "soleil",
		"ordinateur", "pizza", "panda", "fleur", "arbre", "montagne",
		"clé", "fantôme", "robot",
	}
)

func init() { rand.Seed(time.Now().UnixNano()) }

func generateRoomCode() string {
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	for {
		code := make([]rune, 5)
		for i := range code {
			code[i] = rune(letters[rand.Intn(len(letters))])
		}
		str := string(code)
		if _, exists := rooms[str]; !exists {
			return str
		}
	}
}

func broadcast(room *Room, msg Message) {
	room.PlayersMu.Lock()
	conns := make([]*websocket.Conn, 0, len(room.Players))
	for _, p := range room.Players {
		conns = append(conns, p.Conn)
	}
	room.PlayersMu.Unlock()

	data, _ := json.Marshal(msg)
	for _, c := range conns {
		_ = websocket.Message.Send(c, data)
	}
}

func broadcastExcept(room *Room, exclude *Player, msg Message) {
	room.PlayersMu.Lock()
	conns := make([]*websocket.Conn, 0, len(room.Players))
	for _, p := range room.Players {
		if p != exclude {
			conns = append(conns, p.Conn)
		}
	}
	room.PlayersMu.Unlock()

	data, _ := json.Marshal(msg)
	for _, c := range conns {
		_ = websocket.Message.Send(c, data)
	}
}

func broadcastPlayers(room *Room) {
	room.PlayersMu.Lock()
	list := make([]string, 0, len(room.Players))
	for _, p := range room.Players {
		label := fmt.Sprintf("%s (%d pts)", p.Pseudo, p.Score)
		if room.DrawPlayer == p {
			label += " 🎨"
		}
		list = append(list, label)
	}
	host := ""
	if room.Host != nil {
		host = room.Host.Pseudo
	}
	room.PlayersMu.Unlock()

	b, _ := json.Marshal(list)
	broadcast(room, Message{Type: "players", Content: string(b)})
	if host != "" {
		broadcast(room, Message{Type: "host", Content: host})
	}
}

func startGame(room *Room) {
	room.PlayersMu.Lock()
	if len(room.Players) < 2 {
		room.PlayersMu.Unlock()
		broadcast(room, Message{Type: "error", Content: "⚠️ Il faut au moins 2 joueurs pour commencer."})
		return
	}
	room.Round = 1
	room.Started = true
	room.PlayersMu.Unlock()

	fmt.Println("🚀 startGame: OK (players:", len(room.Players), ")")
	broadcast(room, Message{Type: "info", Content: "🚀 La partie commence !"})
	nextTurn(room)
}

func nextTurn(room *Room) {
	room.PlayersMu.Lock()
	if len(room.Players) == 0 {
		room.PlayersMu.Unlock()
		return
	}
	player := room.Players[rand.Intn(len(room.Players))]
	room.DrawPlayer = player
	room.Guessed = make(map[*Player]bool)
	room.PlayersMu.Unlock()

	opts := randomWords()
	payload, _ := json.Marshal(opts)
	_ = websocket.JSON.Send(player.Conn, Message{Type: "choose_word", Content: string(payload)})

	fmt.Println("🖌️ nextTurn: dessinateur =", player.Pseudo)
	broadcast(room, Message{Type: "info", Content: fmt.Sprintf("🖌️ %s choisit un mot...", player.Pseudo)})
	broadcastPlayers(room)
}

func startDrawing(room *Room, word string) {
	room.CurrentWord = strings.ToLower(word)
	room.Guessed = make(map[*Player]bool)

	_ = websocket.JSON.Send(room.DrawPlayer.Conn, Message{Type: "start_drawing", Content: word})
	broadcastExcept(room, room.DrawPlayer, Message{Type: "start_drawing_public", Content: word})
	broadcast(room, Message{Type: "info", Content: fmt.Sprintf("✏️ %s commence à dessiner !", room.DrawPlayer.Pseudo)})

	fmt.Println("🎨 startDrawing:", word, "par", room.DrawPlayer.Pseudo)
	go manageRound(room)
}

func manageRound(room *Room) {
	hintTimes := []int{20, 35, 50}
	hints := []int{1, 2, 3}

	for i, t := range hintTimes {
		time.Sleep(time.Duration(t) * time.Second)
		if room.CurrentWord == "" {
			return
		}
		hint := genHint(room.CurrentWord, hints[i])
		broadcast(room, Message{Type: "hint", Content: hint})
	}

	time.Sleep(10 * time.Second)
	endRound(room)
}

func genHint(word string, letters int) string {
	runes := []rune(word)
	hint := make([]rune, len(runes))
	for i := range hint {
		hint[i] = '_'
	}
	indices := rand.Perm(len(runes))[:letters]
	for _, i := range indices {
		hint[i] = runes[i]
	}
	return strings.Join(strings.Split(string(hint), ""), " ")
}

func endRound(room *Room) {
	if room.CurrentWord == "" {
		return
	}

	broadcast(room, Message{Type: "round_end", Content: room.CurrentWord})
	broadcast(room, Message{Type: "info", Content: fmt.Sprintf("✅ Fin du tour ! Le mot était '%s'.", room.CurrentWord)})

	fmt.Println("🏁 endRound, mot:", room.CurrentWord)

	room.CurrentWord = ""
	room.Round++
	broadcastPlayers(room)

	room.PlayersMu.Lock()
	maxRounds := len(room.Players) * 3
	room.PlayersMu.Unlock()

	if room.Round > maxRounds {
		endGame(room)
	} else {
		time.AfterFunc(4*time.Second, func() { nextTurn(room) })
	}
}

func endGame(room *Room) {
	room.Started = false

	room.PlayersMu.Lock()
	scores := make([]string, 0, len(room.Players))
	for _, p := range room.Players {
		scores = append(scores, fmt.Sprintf("%s : %d pts", p.Pseudo, p.Score))
	}
	room.PlayersMu.Unlock()

	broadcast(room, Message{Type: "game_over", Content: strings.Join(scores, " | ")})
	broadcast(room, Message{Type: "info", Content: "🏁 Partie terminée !"})

	fmt.Println("🏁 endGame:", strings.Join(scores, " | "))
}

func randomWords() []string {
	out := make([]string, 3)
	for i := range out {
		out[i] = words[rand.Intn(len(words))]
	}
	return out
}

func wsHandler(ws *websocket.Conn) {
	fmt.Println("🔗 Nouveau client WS")
	var player *Player
	var room *Room

	defer func() {
		if room != nil {
			removePlayer(room, player)
		}
		_ = ws.Close()
		fmt.Println("🔌 Client WS déconnecté")
	}()

	for {
		var raw []byte
		if err := websocket.Message.Receive(ws, &raw); err != nil {
			return
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		switch msg.Type {

		case "nickname":
			player = &Player{Conn: ws, Pseudo: strings.TrimSpace(msg.Content), JoinedAt: time.Now()}
			fmt.Println("✅ nickname:", player.Pseudo)

		case "create_room":
			if player == nil || player.Pseudo == "" {
				_ = websocket.JSON.Send(ws, Message{Type: "error", Content: "Pseudo manquant"})
				continue
			}
			code := generateRoomCode()
			room = &Room{Code: code, Players: []*Player{player}, Host: player}
			roomsMu.Lock()
			rooms[code] = room
			roomsMu.Unlock()

			_ = websocket.JSON.Send(ws, Message{Type: "room_created", Content: code})
			broadcastPlayers(room)
			broadcast(room, Message{Type: "info", Content: fmt.Sprintf("👋 %s a créé la room.", player.Pseudo)})
			fmt.Println("🏠 create_room:", code, "host:", player.Pseudo)

		case "join_room":
			code := strings.ToUpper(strings.TrimSpace(msg.Content))
			fmt.Println("➡️ join_room reçu pour", code, "par", safePseudo(player))
			roomsMu.Lock()
			r, ok := rooms[code]
			roomsMu.Unlock()
			if !ok {
				_ = websocket.JSON.Send(ws, Message{Type: "error", Content: "❌ Room inconnue"})
				fmt.Println("❌ join_room: room inconnue", code)
				continue
			}
			room = r
			room.PlayersMu.Lock()
			room.Players = append(room.Players, player)
			room.PlayersMu.Unlock()

			_ = websocket.JSON.Send(ws, Message{Type: "room_joined", Content: code})
			broadcastPlayers(room)
			broadcast(room, Message{Type: "info", Content: fmt.Sprintf("👋 %s a rejoint la partie.", player.Pseudo)})
			fmt.Println("✅ join_room OK:", code, "->", player.Pseudo)

		case "start_game":
			if room != nil && player == room.Host {
				fmt.Println("▶️ start_game demandé par", player.Pseudo)
				startGame(room)
			}

		case "choose_word":
			if room != nil && player == room.DrawPlayer {
				fmt.Println("📝 choose_word par", player.Pseudo, "mot:", msg.Content)
				startDrawing(room, msg.Content)
			}

		case "draw", "fill", "undo", "clear":
			if room != nil && player == room.DrawPlayer {
				broadcastExcept(room, player, msg)
			}

		case "message":
			if room == nil || player == nil {
				continue
			}
			text := strings.TrimSpace(msg.Content)
			if text == "" || player == room.DrawPlayer {
				continue 
			}

			if room.CurrentWord != "" && strings.EqualFold(text, room.CurrentWord) && !room.Guessed[player] {
				room.Guessed[player] = true
				points := 50 + rand.Intn(51)
				player.Score += points

				broadcast(room, Message{Type: "chat", Content: fmt.Sprintf("🏆 %s a trouvé le mot ! (+%d)", player.Pseudo, points)})
				broadcastPlayers(room)

				room.PlayersMu.Lock()
				allFound := len(room.Guessed) >= len(room.Players)-1
				room.PlayersMu.Unlock()
				if allFound {
					endRound(room)
				}
			} else {
				broadcast(room, Message{Type: "chat", Content: fmt.Sprintf("%s: %s", player.Pseudo, text)})
			}
		}
	}
}

func safePseudo(p *Player) string {
	if p == nil {
		return "<nil>"
	}
	return p.Pseudo
}

func removePlayer(room *Room, player *Player) {
	if player == nil || room == nil {
		return
	}

	room.PlayersMu.Lock()
	newList := make([]*Player, 0, len(room.Players))
	for _, p := range room.Players {
		if p != player {
			newList = append(newList, p)
		}
	}
	room.Players = newList
	if room.Host == player && len(room.Players) > 0 {
		room.Host = room.Players[0]
	}
	room.PlayersMu.Unlock()

	broadcast(room, Message{Type: "info", Content: fmt.Sprintf("❌ %s a quitté la partie.", player.Pseudo)})
	broadcastPlayers(room)
	fmt.Println("👋 removePlayer:", safePseudo(player))
}

func main() {
	http.Handle("/ws", websocket.Handler(wsHandler))
	http.Handle("/", http.FileServer(http.Dir("web")))
	fmt.Println("✅ Serveur GuessWhat prêt sur :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
