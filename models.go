package main

type Torrent struct {
	Name          string  `json:"name"`
	Progress      float64 `json:"progress"` // fraction from 0 to 1
	State         string  `json:"state"`
	DownloadSpeed int64   `json:"dlspeed"`
	UploadSpeed   int64   `json:"upspeed"`
	ETA           int64   `json:"eta"`
	Size          int64   `json:"size"`
	Seeds         int     `json:"num_seeds"`
	Peers         int     `json:"num_peers"`
}

// Since progress is fraction, to get percent multiply by 100
