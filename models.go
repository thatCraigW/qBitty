package main

type Torrent struct {
	Hash          string  `json:"hash"`
	Name          string  `json:"name"`
	Progress      float64 `json:"progress"` // fraction from 0 to 1
	State         string  `json:"state"`
	DownloadSpeed int64   `json:"dlspeed"`
	UploadSpeed   int64   `json:"upspeed"`
	ETA           int64   `json:"eta"`
	Size          int64   `json:"size"`
	Seeds         int     `json:"num_seeds"`
	Peers         int     `json:"num_peers"`
	AddedOn       int64   `json:"added_on"`
	Category      string  `json:"category"`
	SavePath      string  `json:"save_path"`
	ContentPath   string  `json:"content_path"`
	Tracker       string  `json:"tracker"`
	Ratio         float64 `json:"ratio"`
	Downloaded    int64   `json:"downloaded"`
	Uploaded      int64   `json:"uploaded"`
	AmountLeft    int64   `json:"amount_left"`
	Priority      int     `json:"priority"`
}

type TorrentProperties struct {
	SavePath           string  `json:"save_path"`
	CreationDate       int64   `json:"creation_date"`
	PieceSize          int64   `json:"piece_size"`
	Comment            string  `json:"comment"`
	TotalWasted        int64   `json:"total_wasted"`
	TotalUploaded      int64   `json:"total_uploaded"`
	TotalDownloaded    int64   `json:"total_downloaded"`
	UpLimit            int64   `json:"up_limit"`
	DlLimit            int64   `json:"dl_limit"`
	TimeElapsed        int64   `json:"time_elapsed"`
	NbConnections      int     `json:"nb_connections"`
	NbConnectionsLimit int     `json:"nb_connections_limit"`
	ShareRatio         float64 `json:"share_ratio"`
	AdditionDate       int64   `json:"addition_date"`
	CompletionDate     int64   `json:"completion_date"`
	CreatedBy          string  `json:"created_by"`
	DlSpeedAvg         int64   `json:"dl_speed_avg"`
	UpSpeedAvg         int64   `json:"up_speed_avg"`
	LastSeen           int64   `json:"last_seen"`
	Peers              int     `json:"peers"`
	PeersTotal         int     `json:"peers_total"`
	Seeds              int     `json:"seeds"`
	SeedsTotal         int     `json:"seeds_total"`
	TotalSize          int64   `json:"total_size"`
	PiecesHave         int     `json:"pieces_have"`
	PiecesNum          int     `json:"pieces_num"`
	SeedingTime        int64   `json:"seeding_time"`
}

type Tracker struct {
	URL           string `json:"url"`
	Status        int    `json:"status"`
	Tier          int    `json:"tier"`
	NumPeers      int    `json:"num_peers"`
	NumSeeds      int    `json:"num_seeds"`
	NumLeeches    int    `json:"num_leeches"`
	NumDownloaded int    `json:"num_downloaded"`
	Msg           string `json:"msg"`
}

type Peer struct {
	IP          string  `json:"ip"`
	Port        int     `json:"port"`
	Client      string  `json:"client"`
	Progress    float64 `json:"progress"`
	DlSpeed     int64   `json:"dl_speed"`
	UpSpeed     int64   `json:"up_speed"`
	Downloaded  int64   `json:"downloaded"`
	Uploaded    int64   `json:"uploaded"`
	Connection  string  `json:"connection"`
	Flags       string  `json:"flags"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
}

// PeersResponse wraps the /sync/torrentPeers response which returns peers as a map.
type PeersResponse struct {
	FullUpdate bool            `json:"full_update"`
	Peers      map[string]Peer `json:"peers"`
}

type WebSeed struct {
	URL string `json:"url"`
}

type TorrentFile struct {
	Index        int     `json:"index"`
	Name         string  `json:"name"`
	Size         int64   `json:"size"`
	Progress     float64 `json:"progress"`
	Priority     int     `json:"priority"`
	Availability float64 `json:"availability"`
}
