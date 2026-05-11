package main

import (
	"encoding/json"
	"os"
)

type Configs []Config

type Config struct {
	HTTPListen      string `json:"HTTPListen"`
	HTTPProxy       string `json:"HTTPProxy"`
	HTTPLink        string `json:"HTTPLink"`
	NameSelf        string `json:"NameSelf"`
	NamePeer        string `json:"NamePeer"`
	DefaultAddrSelf string `json:"DefaultAddrSelf"`
	StunAddr        string `json:"StunAddr"`
	StunListURL     string `json:"StunListURL"`
	PublicListen    string `json:"PublicListen"`
	PrivateListen   string `json:"PrivateListen"`
	PrivateSendto   string `json:"PrivateSendto"`
	AutoExpire      int    `json:"AutoExpire"`
	Log             string `json:"Log"`
}

func parseJSONConfig(config *Configs, path string) error {
	file, err := os.Open(path) // For read access.
	if err != nil {
		return err
	}
	defer file.Close()

	return json.NewDecoder(file).Decode(config)
}
