package db

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/gernest/mention"
)

// replace @cosmosaddr with profile link [@username](https://app.trustory.io/profile/cosmosaddr)
func (c *Client) replaceAddressesWithProfileURLs(body string) (string, error) {
	profileURLPrefix := path.Join(c.config.Host.Name, "profile")
	profileURLsByAddress, err := c.mapAddressesToProfileURLs(body, profileURLPrefix)
	if err != nil {
		return "", err
	}
	for address, profileURL := range profileURLsByAddress {
		body = strings.ReplaceAll(body, fmt.Sprintf("@%s", address), profileURL)
	}

	return body, nil
}

func (c *Client) mapAddressesToProfileURLs(body string, profileURLPrefix string) (map[string]string, error) {
	profileURLsByAddress := map[string]string{}
	addresses := parseMentions(body)
	for _, address := range addresses {
		twitterProfile, err := c.TwitterProfileByAddress(address)
		if err != nil {
			return profileURLsByAddress, err
		}
		if twitterProfile == nil {
			profileURLsByAddress[address] = address
			continue
		}
		profileURLString := path.Join(profileURLPrefix, twitterProfile.Address)
		profileURL, err := url.Parse(profileURLString)
		if err != nil {
			return profileURLsByAddress, err
		}

		httpPrefix := "http://"
		if c.config.Host.HTTPSEnabled == true {
			httpPrefix = "https://"
		}
		markdownProfileURL := fmt.Sprintf("[@%s](%s%s)", twitterProfile.Username, httpPrefix, profileURL)
		profileURLsByAddress[address] = markdownProfileURL
	}

	return profileURLsByAddress, nil
}

// extract @mentions from text and return as slice
func parseMentions(body string) []string {
	return mention.GetTagsAsUniqueStrings('@', body, ' ', '\n', '\r')
}

// replace @usernames with @cosmosaddr
func (c *Client) replaceUsernamesWithAddress(body string) (string, error) {
	addressByUsername := map[string]string{}
	usernames := parseMentions(body)
	for _, username := range usernames {
		twitterProfile, err := c.TwitterProfileByUsername(username)
		if err != nil {
			return body, err
		}
		if twitterProfile == nil {
			addressByUsername[username] = username
		} else {
			addressByUsername[username] = twitterProfile.Address
		}
	}
	for username, address := range addressByUsername {
		body = strings.ReplaceAll(body, username, address)
	}

	return body, nil
}

// TranslateToCosmosMentions translates from users mentions to cosmos addresses mentions.
func (c *Client) TranslateToCosmosMentions(body string) (string, error) {
	return c.replaceUsernamesWithAddress(body)
}

// TranslateToUsersMentions translates from cosmos addresses mentions to users mentions.
func (c *Client) TranslateToUsersMentions(body string) (string, error) {
	return c.replaceAddressesWithProfileURLs(body)
}