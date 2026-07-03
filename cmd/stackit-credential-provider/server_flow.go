package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

const metadataURL = "http://169.254.169.254/stackit/v1/service-accounts"

var _ tokenGetter = (*serverFlow)(nil)

type serverFlow struct{}

type listResponse struct {
	ServiceAccountMails []string `json:"serviceAccountMails"`
}

type tokenResponse struct {
	Token string `json:"token"`
}

// GetAccessToken implements [tokenGetter].
func (s *serverFlow) GetAccessToken() (string, error) {
	mail, err := getServiceAccountMail()
	if err != nil {
		return "", err
	}

	token, err := getToken(mail)
	if err != nil {
		return "", err
	}
	return token, nil
}

func getToken(mail string) (string, error) {
	tokenURL, err := url.JoinPath(metadataURL, mail, "token")
	if err != nil {
		return "", err
	}
	resp, err := http.Get(tokenURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	tokenResp := tokenResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}
	return tokenResp.Token, nil
}

func getServiceAccountMail() (string, error) {
	resp, err := http.Get(metadataURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	listResp := listResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return "", err
	}

	if len(listResp.ServiceAccountMails) == 0 {
		return "", errors.New("no service account attached to this server")
	}

	return listResp.ServiceAccountMails[0], nil
}
