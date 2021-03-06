package db

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/TruStory/octopus/services/truapi/truapi/regex"
	"github.com/go-pg/pg"
	"golang.org/x/crypto/bcrypt"
)

// InvitedUserDefaultName is the default name given to the invited user
const InvitedUserDefaultName = "<invited user>"

// StepsToCompleteJourney denotes the number of steps one user has to complete to be considered active
const StepsToCompleteJourney = 4 // signup, write an argument, receive five agrees, give an agree

type UserGroup int

const (
	// UserGroupUser is just a regular user and the default group assignation
	UserGroupUser = iota
	UserGroupEmployee
	UserGroupTruStoryDebater
	UserGroupResearchAnalyst
)

var userGroupTypeName = []string{
	UserGroupUser:            "User",
	UserGroupEmployee:        "Employee",
	UserGroupTruStoryDebater: "TruStory Debater",
	UserGroupResearchAnalyst: "Research Analyst",
}

func (ug UserGroup) String() string {
	if int(ug) >= len(userGroupTypeName) {
		return "Unknown"
	}
	return userGroupTypeName[ug]
}

// User is the user on the TruStory platform
type User struct {
	Timestamps

	ID                        int64      `json:"id"`
	FullName                  string     `json:"full_name"`
	Username                  string     `json:"username"`
	Email                     string     `json:"email"`
	Bio                       string     `json:"bio"`
	AvatarURL                 string     `json:"avatar_url"`
	Address                   string     `json:"address"`
	InvitesLeft               int64      `json:"invites_left"`
	Password                  string     `json:"-" graphql:"-"`
	ReferredBy                int64      `json:"referred_by"`
	Token                     string     `json:"-" graphql:"-"`
	ApprovedAt                time.Time  `json:"approved_at" graphql:"-"`
	RejectedAt                time.Time  `json:"rejected_at" graphql:"-"`
	VerifiedAt                time.Time  `json:"verified_at" graphql:"-"`
	BlacklistedAt             time.Time  `json:"blacklisted_at" graphql:"-"`
	LastAuthenticatedAt       *time.Time `json:"last_authenticated_at" graphql:"-"`
	UserGroup                 UserGroup  `json:"user_group"`
	LastVerificationAttemptAt time.Time  `json:"last_verification_attempt_at" graphql:"-"`
	VerificationAttemptCount  int        `json:"verification_attempt_count"`
	Meta                      UserMeta   `json:"meta"`
}

// UserMeta holds user meta data
type UserMeta struct {
	OnboardFollowCommunities *bool             `json:"onboardFollowCommunities,omitempty"`
	OnboardCarousel          *bool             `json:"onboardCarousel,omitempty"`
	OnboardContextual        *bool             `json:"onboardContextual,omitempty"`
	Journey                  []UserJourneyStep `json:"journey,omitempty"`
}

// UserJourneyStep is a step in the entire journey
type UserJourneyStep string

const (
	JourneyStepSignedUp          UserJourneyStep = "signed_up"
	JourneyStepOneArgument       UserJourneyStep = "one_argument"
	JourneyStepGivenOneAgree     UserJourneyStep = "given_one_agree"
	JourneyStepReceiveFiveAgrees UserJourneyStep = "received_five_agrees"
)

// UserProfile contains the fields that make up the user profile
type UserProfile struct {
	FullName  string `json:"full_name"`
	Bio       string `json:"bio"`
	AvatarURL string `json:"avatar_url"`
	Username  string `json:"username"`
}

// UserPassword contains the fields that allows users to update their passwords
type UserPassword struct {
	Current         string `json:"current"`
	New             string `json:"new"`
	NewConfirmation string `json:"new_confirmation"`
}

// UserCredentials contains the fields that allows users to log into their accounts
type UserCredentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// UserByID selects a user by ID
func (c *Client) UserByID(ID int64) (*User, error) {
	user := new(User)
	err := c.Model(user).Where("id = ?", ID).First()
	if err == pg.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return user, nil
}

// UserByEmailOrUsername selects a user either by email or username
func (c *Client) UserByEmailOrUsername(identifier string) (*User, error) {
	if regex.IsValidEmail(identifier) {
		return c.UserByEmail(identifier)
	}

	if regex.IsValidUsername(identifier) {
		return c.UserByUsername(identifier)
	}

	return nil, errors.New("no such user")
}

// UserByEmail returns the user using email
func (c *Client) UserByEmail(email string) (*User, error) {
	var user User
	err := c.Model(&user).
		Where("LOWER(email) = ?", strings.ToLower(email)).
		Where("deleted_at IS NULL").
		First()

	if err == pg.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &user, nil
}

// UserByUsername returns the user using username
func (c *Client) UserByUsername(username string) (*User, error) {
	var user User
	err := c.Model(&user).
		Where("LOWER(username) = ?", strings.ToLower(username)).
		Where("deleted_at IS NULL").
		First()

	if err == pg.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &user, nil
}

// UserByAddress returns the verified user using address
func (c *Client) UserByAddress(address string) (*User, error) {
	var user User
	err := c.Model(&user).
		Where("address = ?", address).
		Where("deleted_at IS NULL").
		First()

	if err == pg.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &user, nil
}

// VerifiedUserByID returns the verified user by ID
func (c *Client) VerifiedUserByID(id int64) (*User, error) {
	var user User
	err := c.Model(&user).
		Where("id = ?", id).
		Where("verified_at IS NOT NULL").
		Where("deleted_at IS NULL").
		First()

	if err == pg.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &user, nil
}

// GetAuthenticatedUser authenticates the user and returns the authenticated user
func (c *Client) GetAuthenticatedUser(identifier, password string) (*User, error) {
	user, err := c.UserByEmailOrUsername(identifier)

	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("Invalid username, email or password")
	}

	if !user.BlacklistedAt.IsZero() {
		log.Println("The user is blacklisted and cannot be authenticated", identifier)
		return nil, errors.New("User cannot be authenticated")
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		return nil, errors.New("Oops! Invalid credentials provided")
	}

	return user, nil
}

// TouchLastAuthenticatedAt updates the last_authenticated_at column with the current timestamp
func (c *Client) TouchLastAuthenticatedAt(id int64) error {
	var user User
	_, err := c.Model(&user).
		Where("id = ?", id).
		Where("deleted_at IS NULL").
		Set("last_authenticated_at = ?", time.Now()).
		Update()
	if err != nil {
		return err
	}
	return nil
}

// SetUserMeta updates the meta column
func (c *Client) SetUserMeta(id int64, meta *UserMeta) error {
	var user User
	_, err := c.Model(&user).
		Where("id = ?", id).
		Where("deleted_at IS NULL").
		Set("meta = meta || ?", meta).
		Update()
	if err != nil {
		return err
	}
	return nil
}

// RegisterUser signs up a new user
func (c *Client) RegisterUser(user *User, referrerCode, defaultAvatarURL string) error {
	token, err := generateCryptoSafeRandomBytes(32)
	if err != nil {
		return err
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user.AvatarURL = defaultAvatarURL
	user.Password = string(hashedPassword)
	user.Token = hex.EncodeToString(token)
	user.ApprovedAt = time.Now()
	user.LastVerificationAttemptAt = time.Now()
	user.VerificationAttemptCount = 1

	referrer, err := c.UserByAddress(referrerCode)
	if err != nil {
		return err
	}
	if referrer != nil {
		consumed, err := c.ConsumeInvite(referrer.ID)
		if err != nil {
			return err
		}

		if consumed {
			user.ReferredBy = referrer.ID
		}
	}

	err = c.AddUser(user)
	if err != nil {
		return err
	}

	return nil
}

// VerifyUser verifies the user via token
func (c *Client) VerifyUser(id int64, token string) error {
	var user User
	result, err := c.Model(&user).
		Where("id = ?", id).
		Where("token = ?", token).
		Where("verified_at IS NULL").
		Where("deleted_at IS NULL").
		Set("verified_at = ?", time.Now()).
		Update()
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return errors.New("invalid token")
	}

	return nil
}

// AddAddressToUser adds a cosmos address to the user
func (c *Client) AddAddressToUser(id int64, address string) error {
	var user User
	_, err := c.Model(&user).
		Where("id = ?", id).
		Where("deleted_at IS NULL").
		Set("address = ?", address).
		Update()

	if err != nil {
		return err
	}

	return nil
}

// ResetPassword resets the user's password to a new one
func (c *Client) ResetPassword(id int64, password string) error {
	user, err := c.VerifiedUserByID(id)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("no such user found")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	_, err = c.Model(user).
		Where("id = ?", id).
		Where("deleted_at IS NULL").
		Set("password = ?", string(hashedPassword)).
		Update()

	if err != nil {
		return err
	}

	return nil
}

// UpdatePassword changes a password for a user
func (c *Client) UpdatePassword(id int64, password *UserPassword) error {
	user, err := c.VerifiedUserByID(id)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("no such user found")
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password.Current))
	if err != nil {
		return errors.New("incorrect current password")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password.New), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	_, err = c.Model(user).
		Where("id = ?", id).
		Where("verified_at IS NOT NULL").
		Where("deleted_at IS NULL").
		Set("password = ?", string(hashedPassword)).
		Update()

	if err != nil {
		return err
	}

	return nil
}

// UpdateProfile changes a profile fields for a user
func (c *Client) UpdateProfile(id int64, profile *UserProfile) error {
	user, err := c.UserByID(id)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("no such user found")
	}

	user, err = c.UserByUsername(profile.Username)
	if err != nil {
		return errors.New("no such user found")
	}

	if user != nil && user.ID != id {
		return errors.New("this username has already been taken, please choose another")
	}

	if profile.FullName == "" {
		return errors.New("name cannot be left blank")
	}

	if profile.Username == "" {
		return errors.New("username cannot be left blank")
	}

	if len(profile.Bio) > 160 {
		return errors.New("the bio is too long")
	}

	_, err = c.Model(user).
		Where("id = ?", id).
		Where("deleted_at IS NULL").
		Set("full_name = ?", profile.FullName).
		Set("username = ?", profile.Username).
		Set("bio = ?", profile.Bio).
		Set("avatar_url = ?", profile.AvatarURL).
		Update()

	if err != nil {
		return err
	}

	return nil
}

// SetUserCredentials adds an email + password combo to an existing user, who was previously authorized via some connected account
func (c *Client) SetUserCredentials(id int64, credentials *UserCredentials) error {
	user, err := c.UserByID(id)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("no such user found")
	}
	if !user.VerifiedAt.IsZero() {
		return errors.New("this user already has credentials set and verified")
	}

	hashedPassword, err := getHashedPassword(credentials.Password)
	if err != nil {
		return nil
	}

	_, err = c.Model(user).
		Where("id = ?", id).
		Where("verified_at IS NULL").
		Where("deleted_at IS NULL").
		Set("email = ?", credentials.Email).
		Set("password = ?", hashedPassword).
		Update()

	if err != nil {
		return err
	}

	return nil
}

// ApproveUserByID approves a user to register (set their password + username)
func (c *Client) ApproveUserByID(id int64) error {
	user := new(User)
	_, err := c.Model(user).
		Where("id = ?", id).
		Where("verified_at IS NULL"). // the flag can be updated only until the user hasn't signed up
		Set("approved_at = NOW()").
		Set("rejected_at = NULL").
		Update()

	if err != nil {
		return err
	}

	return nil
}

// RejectUserByID rejects a user from signing up (set their password + username)
func (c *Client) RejectUserByID(id int64) error {
	user := new(User)
	_, err := c.Model(user).
		Where("id = ?", id).
		Where("verified_at IS NULL"). // the flag can be updated only until the user hasn't signed up
		Set("rejected_at = ?", time.Now()).
		Set("approved_at = NULL").
		Update()

	if err != nil {
		return err
	}

	return nil
}

// AddUser upserts the user into the database
func (c *Client) AddUser(user *User) error {
	user.Email = strings.ToLower(user.Email)
	inserted, err := c.Model(user).
		Where("LOWER(email) = ?", user.Email).
		WhereOr("LOWER(username) = ?", strings.ToLower(user.Username)).
		OnConflict("DO NOTHING").
		SelectOrInsert()

	if !inserted {
		return errors.New("a user already exists with same email/username")
	}

	return err
}

// BlacklistUser blacklists a user and prevents them from logging in
func (c *Client) BlacklistUser(id int64) error {
	var user User
	result, err := c.Model(&user).
		Where("id = ?", id).
		Set("blacklisted_at = ?", time.Now()).
		Update()
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return errors.New("invalid user")
	}

	return nil
}

// UnblacklistUser unblacklists a user and allows them from logging in again
func (c *Client) UnblacklistUser(id int64) error {
	var user User
	result, err := c.Model(&user).
		Where("id = ?", id).
		Set("blacklisted_at = NULL").
		Update()
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return errors.New("invalid user")
	}

	return nil
}

// ReferredUsers returns all the users who are invited
func (c *Client) ReferredUsers() ([]User, error) {
	var referredUsers = make([]User, 0)
	err := c.Model(&referredUsers).
		Where("referred_by IS NOT NULL").
		Where("deleted_at IS NULL").
		Select()
	if err != nil {
		return referredUsers, err
	}

	return referredUsers, nil
}

// ReferredUsersByID returns all the users who are invited by a particular user
func (c *Client) ReferredUsersByID(referrerID int64) ([]User, error) {
	var referredUsers = make([]User, 0)
	err := c.Model(&referredUsers).
		Where("deleted_at IS NULL").
		Where("referred_by = ?", referrerID).
		Select()
	if err != nil {
		return referredUsers, err
	}

	return referredUsers, nil
}

// AddUserViaConnectedAccount adds a new user using a new connected account
func (c *Client) AddUserViaConnectedAccount(connectedAccount *ConnectedAccount, referrerCode string) (*User, error) {
	// a.) check if their email address is associated with an existing account.
	// if yes, merge them with that account
	if connectedAccount.Meta.Email != "" {
		user, err := c.UserByEmail(connectedAccount.Meta.Email)
		if err != nil {
			return nil, err
		}
		if user != nil {
			connectedAccount.UserID = user.ID
			err = c.UpsertConnectedAccount(connectedAccount)
			if err != nil {
				return nil, err
			}

			return user, nil
		}
	}

	// b.) if no existing account found, continue creating a new account
	// (if the their connected account's username is not available on the platform,
	// we'll create a random one for them that they can edit later.)
	username, err := getUniqueUsername(c, connectedAccount.Meta.Username, "")
	if err != nil {
		return nil, err
	}
	token, err := generateCryptoSafeRandomBytes(32)
	if err != nil {
		return nil, err
	}
	user := &User{
		FullName:   connectedAccount.Meta.FullName,
		Username:   username,
		Email:      strings.ToLower(connectedAccount.Meta.Email),
		Bio:        connectedAccount.Meta.Bio,
		AvatarURL:  connectedAccount.Meta.AvatarURL,
		Token:      hex.EncodeToString(token),
		ApprovedAt: time.Now(),
	}

	// setting referrer, if any
	referrer, err := c.UserByAddress(referrerCode)
	if err != nil {
		return nil, err
	}
	if referrer != nil {
		consumed, err := c.ConsumeInvite(referrer.ID)
		if err != nil {
			return nil, err
		}

		if consumed {
			user.ReferredBy = referrer.ID
		}
	}

	err = c.AddUser(user)
	if err != nil {
		return nil, err
	}

	connectedAccount.UserID = user.ID
	err = c.UpsertConnectedAccount(connectedAccount)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// UserByConnectedAccountTypeAndID returns the user that has a given connected account
func (c *Client) UserByConnectedAccountTypeAndID(accountType, accountID string) (*User, error) {
	connectedAccount, err := c.ConnectedAccountByTypeAndID(accountType, accountID)
	if err != nil {
		return nil, err
	}

	user := &User{ID: connectedAccount.UserID}
	err = c.Find(user)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// IsTwitterUser returns a twitter user that has a given connected account
func (c *Client) IsTwitterUser(userID int64) bool {
	connectedAccount, err := c.ConnectedAccountByTypeAndUserID("twitter", userID)
	if err != nil {
		return false
	}
	return connectedAccount != nil
}

func getUniqueUsername(c *Client, username string, suffix string) (string, error) {
	candidate := username + suffix
	user, err := c.UserByUsername(username + suffix)
	if err != nil {
		return "", err
	}
	if user != nil {
		intSuffix := 0
		if suffix != "" {
			intSuffix, err = strconv.Atoi(suffix)
			if err != nil {
				return "", err
			}
		}
		return getUniqueUsername(c, username, strconv.Itoa(intSuffix+1))
	}

	return candidate, nil
}

type UsernameAndImage struct {
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url"`
}

// UsernamesAndImagesByPrefix returns the first five usernames and their corresponding images for the provided prefix string
func (c *Client) UsernamesAndImagesByPrefix(prefix string) (usernames []UsernameAndImage, err error) {
	var users []User
	sqlFragment := fmt.Sprintf("username ILIKE '%s", prefix)
	err = c.Model(&users).Where(sqlFragment + "%'").Limit(5).Select()
	if err == pg.ErrNoRows {
		return usernames, nil
	}
	if err != nil {
		return usernames, err
	}
	for _, user := range users {
		obj := UsernameAndImage{
			Username:  user.Username,
			AvatarURL: user.AvatarURL,
		}

		usernames = append(usernames, obj)
	}

	return usernames, nil
}

// UserProfileByAddress fetches user profile details by address
func (c *Client) UserProfileByAddress(addr string) (*UserProfile, error) {
	userProfile := new(UserProfile)
	user := new(User)
	err := c.Model(user).Where("address = ?", addr).Select()
	if err == pg.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return userProfile, err
	}

	userProfile = &UserProfile{
		FullName:  user.FullName,
		Bio:       user.Bio,
		AvatarURL: user.AvatarURL,
		Username:  user.Username,
	}

	return userProfile, nil
}

// UsersByAddress fetches a list of users by address
func (c *Client) UsersByAddress(addresses []string) ([]User, error) {
	users := make([]User, 0)
	err := c.Model(&users).WhereIn("address in (?)", pg.In(addresses)).Select()
	if err == pg.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return users, nil
}

// UsersByID fetches a list of users by id
func (c *Client) UsersByID(ids []int64) ([]User, error) {
	users := make([]User, 0)
	err := c.Model(&users).WhereIn("id in (?)", pg.In(ids)).Select()
	if err == pg.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return users, nil
}

// UserProfileByUsername fetches user profile by username
func (c *Client) UserProfileByUsername(username string) (*UserProfile, error) {
	userProfile := new(UserProfile)
	user := new(User)
	err := c.Model(user).Where("username = ?", username).First()
	if err == pg.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return userProfile, err
	}

	userProfile = &UserProfile{
		FullName:  user.FullName,
		Bio:       user.Bio,
		AvatarURL: user.AvatarURL,
		Username:  user.Username,
	}

	return userProfile, nil
}

// GrantInvites grants the given number of invites to the user
func (c *Client) GrantInvites(id int64, count int) error {
	user := new(User)
	_, err := c.Model(user).
		Where("id = ?", id).
		Set("invites_left = invites_left + ?", count).
		Update()

	if err != nil {
		return err
	}

	_, err = c.RecordRewardLedgerEntry(id, RewardLedgerEntryDirectionCredit, int64(count), RewardLedgerEntryCurrencyInvite)
	if err != nil {
		return err
	}

	return nil
}

// ConsumeInvite consumes one invite, if available
func (c *Client) ConsumeInvite(id int64) (bool, error) {
	user := new(User)
	result, err := c.Model(user).
		Where("id = ?", id).
		Where("invites_left > 0"). // must have atleast one invite left to be consumed
		Set("invites_left = invites_left - 1").
		Update()
	if err != nil {
		return false, err
	}

	if result.RowsAffected() == 0 {
		return false, nil
	}

	_, err = c.RecordRewardLedgerEntry(id, RewardLedgerEntryDirectionDebit, 1, RewardLedgerEntryCurrencyInvite)
	if err != nil {
		return false, err
	}

	return true, nil
}

// UsersWithIncompleteJourney returns all the users who have not yet completed their journey
func (c *Client) UsersWithIncompleteJourney() ([]User, error) {
	var users = make([]User, 0)
	err := c.Model(&users).
		Where("meta ? 'journey' = false").
		WhereOr("jsonb_array_length(meta->'journey') < ?", StepsToCompleteJourney).
		Select()
	if err != nil {
		return users, err
	}

	return users, nil
}

// UpdateUserJourney updates the user journey
func (c *Client) UpdateUserJourney(id int64, journey []UserJourneyStep) error {
	user, err := c.UserByID(id)
	if err != nil {
		return err
	}
	meta := user.Meta
	meta.Journey = journey

	err = c.SetUserMeta(id, &meta)
	if err != nil {
		return err
	}

	return nil
}

// UnverifiedUsersWithinDays returns unverified new users (7 days threshold)
func (c *Client) UnverifiedUsersWithinDays(days int64) ([]User, error) {
	users := make([]User, 0)
	err := c.Model(&users).
		Where("blacklisted_at IS NULL").                       // not blacklisted
		Where("password IS NOT NULL").                         // not the twitter user
		Where("verified_at IS NULL").                          // not yet verified
		Where("created_at > NOW() - interval '? days'", days). // is new
		Select()
	if err != nil {
		return users, err
	}

	return users, nil
}

// RecordVerificationAttempt records a verification attempt
func (c *Client) RecordVerificationAttempt(id int64) error {
	var user User
	_, err := c.Model(&user).
		Where("id = ?", id).
		Set("last_verification_attempt_at = NOW()").
		Set("verification_attempt_count = verification_attempt_count + 1").
		Update()

	if err != nil {
		return err
	}

	return nil
}

func getHashedPassword(password string) (string, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	return string(hashedPassword), nil
}
