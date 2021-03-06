package regex

import (
	"regexp"
)

// RegexValidEmail for valid email
// https://play.golang.org/p/63TNM7ZtiwT
var RegexValidEmail = regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")

// RegexValidUsername for valid username
var RegexValidUsername = regexp.MustCompile("^[a-zA-Z0-9_]{1,28}$")

// RegexHasTrustory for finding trustory in the strings
// https://play.golang.org/p/NrZWfW5LgSr
var RegexHasTrustory = regexp.MustCompile("(?i)trustory")

// Some helper methods based on the above regex

// IsValidEmail returns whether an email matches the valid email regex or not
func IsValidEmail(email string) bool {
	return RegexValidEmail.MatchString(email)
}

// IsValidUsername returns whether an username matches the valid username regex or not
func IsValidUsername(username string) bool {
	return RegexValidUsername.MatchString(username)
}

// HasTrustory returns whether the string contains the brand name in it or not
func HasTrustory(str string) bool {
	return RegexHasTrustory.MatchString(str)
}
