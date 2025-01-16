// main.go
package main

import (
    "encoding/json"
    "fmt"
    "html/template"
    "log"
    "net/http"
    "sort"
    "sync"

    "github.com/gorilla/mux"
    "github.com/zmb3/spotify"
)

var (
    auth  spotify.Authenticator
    ch    = make(chan *spotify.Client)
    state = "abc123"
    templates *template.Template
    voteMutex sync.RWMutex
    albumVotes = make(map[string]int)
)

type PageData struct {
    Albums []AlbumData
}

type AlbumData struct {
    ID       string
    Name     string
    Artist   string
    ImageURL string
    Votes    int
}

func init() {
    // Initialize Spotify auth
    auth = spotify.NewAuthenticator(
        "http://localhost:8080/callback",
        spotify.ScopeUserLibraryRead,
    )
    
    // Load templates
    templates = template.Must(template.ParseGlob("templates/*.html"))
}

func main() {
    // Set up router
    r := mux.NewRouter()
    
    // Routes
    r.HandleFunc("/", handleHome)
    r.HandleFunc("/login", handleLogin)
    r.HandleFunc("/callback", handleCallback)
    r.HandleFunc("/vote/{id}", handleVote).Methods("POST")
    r.HandleFunc("/top", handleTop)
    r.HandleFunc("/reset", handleReset).Methods("POST")
    
    // Serve static files
    r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
    
    // Start server
    fmt.Println("Server starting on :8080")
    log.Fatal(http.ListenAndServe(":8080", r))
}

func handleHome(w http.ResponseWriter, r *http.Request) {
    if r.URL.Path != "/" {
        http.NotFound(w, r)
        return
    }
    templates.ExecuteTemplate(w, "login.html", nil)
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
    url := auth.AuthURL(state)
    http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
    tok, err := auth.Token(state, r)
    if err != nil {
        http.Error(w, "Couldn't get token", http.StatusForbidden)
        return
    }
    
    client := auth.NewClient(tok)
    albums, err := fetchLikedAlbums(&client)
    if err != nil {
        http.Error(w, "Error fetching albums", http.StatusInternalServerError)
        return
    }
    
    // Add votes to albums
    voteMutex.RLock()
    for i := range albums {
        albums[i].Votes = albumVotes[albums[i].ID]
    }
    voteMutex.RUnlock()
    
    templates.ExecuteTemplate(w, "albums.html", PageData{Albums: albums})
}

func fetchLikedAlbums(client *spotify.Client) ([]AlbumData, error) {
    var albums []AlbumData
    limit := 50
    offset := 0
    
    for {
        savedAlbums, err := client.CurrentUsersAlbumsOpt(&spotify.Options{
            Limit:  &limit,
            Offset: &offset,
        })
        if err != nil {
            return nil, err
        }
		for _, savedAlbum := range savedAlbums.Albums {
            var imageURL string
            if len(savedAlbum.SimpleAlbum.Images) > 0 {
                imageURL = savedAlbum.SimpleAlbum.Images[0].URL
            }

            albums = append(albums, AlbumData{
                ID:       savedAlbum.SimpleAlbum.ID.String(),
                Name:     savedAlbum.SimpleAlbum.Name,
                Artist:   savedAlbum.SimpleAlbum.Artists[0].Name,
                ImageURL: imageURL,
            })
        }

        if len(savedAlbums.Albums) < limit {
            break
        }
        offset += limit
    }
    
    return albums, nil
}

func handleVote(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    albumID := vars["id"]
    
    voteMutex.Lock()
    albumVotes[albumID]++
    votes := albumVotes[albumID]
    voteMutex.Unlock()
    
    json.NewEncoder(w).Encode(map[string]int{"votes": votes})
}

func handleTop(w http.ResponseWriter, r *http.Request) {
    voteMutex.RLock()
    type albumVote struct {
        ID    string
        Votes int
    }
    
    var votes []albumVote
    for id, count := range albumVotes {
        votes = append(votes, albumVote{ID: id, Votes: count})
    }
    
    sort.Slice(votes, func(i, j int) bool {
        return votes[i].Votes > votes[j].Votes
    })
    
    // Get top 10
    if len(votes) > 10 {
        votes = votes[:10]
    }
    voteMutex.RUnlock()
    
    templates.ExecuteTemplate(w, "top.html", votes)
}

func handleReset(w http.ResponseWriter, r *http.Request) {
    voteMutex.Lock()
    albumVotes = make(map[string]int)
    voteMutex.Unlock()
    
    http.Redirect(w, r, "/top", http.StatusSeeOther)
}
