package truapi

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	app "github.com/TruStory/truchain/types"
	"github.com/TruStory/truchain/x/bank"
	"github.com/TruStory/truchain/x/claim"
	"github.com/TruStory/truchain/x/community"
	"github.com/TruStory/truchain/x/slashing"
	"github.com/TruStory/truchain/x/staking"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gorilla/mux"

	"github.com/TruStory/octopus/services/truapi/chttp"
	truCtx "github.com/TruStory/octopus/services/truapi/context"
	"github.com/TruStory/octopus/services/truapi/db"
	"github.com/TruStory/octopus/services/truapi/dripper"
	"github.com/TruStory/octopus/services/truapi/graphql"
	"github.com/TruStory/octopus/services/truapi/postman"
	"github.com/TruStory/octopus/services/truapi/truapi/cookies"
)

// ContextKey represents a string key for request context.
type ContextKey string

const (
	userContextKey        = ContextKey("user")
	dataLoadersContextKey = ContextKey("dataLoaders")
)

const (
	// Bech32PrefixAccAddr defines the Bech32 prefix of an account's address
	Bech32PrefixAccAddr = "tru"
	// Bech32PrefixAccPub defines the Bech32 prefix of an account's public key
	Bech32PrefixAccPub = "trupub"
	// Bech32PrefixValAddr defines the Bech32 prefix of a validator's operator address
	Bech32PrefixValAddr = "truvaloper"
	// Bech32PrefixValPub defines the Bech32 prefix of a validator's operator public key
	Bech32PrefixValPub = "truvaloperpub"
	// Bech32PrefixConsAddr defines the Bech32 prefix of a consensus node address
	Bech32PrefixConsAddr = "truvalcons"
	// Bech32PrefixConsPub defines the Bech32 prefix of a consensus node public key
	Bech32PrefixConsPub = "truvalconspub"
)

type dataLoaders struct {
	appAccountLoader  *AppAccountLoader
	userProfileLoader *UserProfileLoader
}

// TruAPI implements an HTTP server for TruStory functionality using `chttp.API`
type TruAPI struct {
	*chttp.API
	APIContext    truCtx.TruAPIContext
	GraphQLClient *graphql.Client
	DBClient      db.Datastore
	Postman       *postman.Postman
	Dripper       *dripper.Dripper

	// notifications
	notificationsInitialized bool
	commentsNotificationsCh  chan CommentNotificationRequest
	broadcastNotificationsCh chan BroadcastNotificationRequest
	httpClient               *http.Client
}

// NewTruAPI returns a `TruAPI` instance populated with the existing app and a new GraphQL client
func NewTruAPI(apiCtx truCtx.TruAPIContext) *TruAPI {
	// Read in the configuration file for the sdk
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount(Bech32PrefixAccAddr, Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(Bech32PrefixValAddr, Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(Bech32PrefixConsAddr, Bech32PrefixConsPub)
	config.Seal()

	postmanService, err := postman.NewPostman(apiCtx.Config)
	if err != nil {
		log.Fatal(err)
	}
	dripperService, err := dripper.NewDripper(apiCtx.Config)
	if err != nil {
		log.Fatal(err)
	}
	ta := TruAPI{
		API:                      chttp.NewAPI(apiCtx, supported),
		APIContext:               apiCtx,
		GraphQLClient:            graphql.NewGraphQLClient(),
		DBClient:                 db.NewDBClient(apiCtx.Config),
		Postman:                  postmanService,
		Dripper:                  dripperService,
		commentsNotificationsCh:  make(chan CommentNotificationRequest),
		broadcastNotificationsCh: make(chan BroadcastNotificationRequest),
		httpClient: &http.Client{
			Timeout: time.Second * 5,
		},
	}

	return &ta
}

// RunNotificationSender connects to the push notification service
func (ta *TruAPI) RunNotificationSender(apiCtx truCtx.TruAPIContext) error {
	ta.notificationsInitialized = true
	go ta.runCommentNotificationSender(ta.commentsNotificationsCh, apiCtx.Config.Push.EndpointURL)
	go ta.runBroadcastNotificationSender(ta.broadcastNotificationsCh, apiCtx.Config.Push.EndpointURL)
	return nil
}

// RunLeaderboardScheduler runs the leaderboard background processing.
func (ta *TruAPI) RunLeaderboardScheduler(apiCtx truCtx.TruAPIContext) {
	go ta.leaderboardScheduler()
}

// WrapHandler wraps a chttp.Handler and returns a standar http.Handler
func WrapHandler(h chttp.Handler) http.Handler {
	return h.HandlerFunc()
}

// WithUser sets the user in the context that will be passed down to handlers.
func WithUser(apiCtx truCtx.TruAPIContext) mux.MiddlewareFunc {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth, err := cookies.GetAuthenticatedUser(apiCtx, r)
			if err != nil {
				h.ServeHTTP(w, r)
				return
			}
			ctx := context.WithValue(r.Context(), userContextKey, auth)
			h.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func (ta *TruAPI) createContext(ctx context.Context) context.Context {
	loaders := &dataLoaders{
		appAccountLoader:  ta.AppAccountLoader(),
		userProfileLoader: ta.UserProfileLoader(),
	}
	return context.WithValue(ctx, dataLoadersContextKey, loaders)
}

func (ta *TruAPI) WithDataLoaders() mux.MiddlewareFunc {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r.WithContext(ta.createContext(r.Context())))
		})
	}
}

func getDataLoaders(ctx context.Context) (*dataLoaders, bool) {
	l, ok := ctx.Value(dataLoadersContextKey).(*dataLoaders)
	if !ok || l == nil {
		return nil, false
	}
	return l, true
}

// BasicAuth wraps a handler requiring HTTP basic auth for it using the given
// username and password and the specified realm, which shouldn't contain quotes.
//
// Most web browser display a dialog with something like:
//
//    The website says: "<realm>"
//
// Which is really stupid so you may want to set the realm to a message rather than
// an actual realm.
func BasicAuth(apiCtx truCtx.TruAPIContext, handler http.Handler) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(apiCtx.Config.Admin.Username)) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(apiCtx.Config.Admin.Password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm=please authenticate`)
			w.WriteHeader(401)
			_, _ = w.Write([]byte("Unauthorised.\n"))
			return
		}

		handler.ServeHTTP(w, r)
	})
}

// RegisterMutations registers mutations
func (ta *TruAPI) RegisterMutations() {
	ta.GraphQLClient.RegisterMutation("addComment", func(args struct {
		Parent int64
		Body   string
	}) error {
		err := ta.DBClient.AddComment(&db.Comment{ParentID: args.Parent, Body: args.Body})
		return err
	})
}

// RegisterResolvers builds the app's GraphQL schema from resolvers (declared in `resolver.go`)
func (ta *TruAPI) RegisterResolvers() {
	ta.GraphQLClient.RegisterObjectResolver("Reaction", db.Reaction{}, map[string]interface{}{
		"id":   func(_ context.Context, q db.Reaction) int64 { return q.ID },
		"type": func(_ context.Context, q db.Reaction) db.ReactionType { return q.ReactionType },
		"creator": func(ctx context.Context, q db.Reaction) *AppAccount {
			return ta.appAccountResolver(ctx, queryByAddress{ID: q.Creator})
		},
	})

	ta.GraphQLClient.RegisterObjectResolver("Coin", sdk.Coin{}, map[string]interface{}{
		"amount":        func(_ context.Context, q sdk.Coin) string { return q.Amount.String() },
		"denom":         func(_ context.Context, q sdk.Coin) string { return q.Denom },
		"humanReadable": func(_ context.Context, q sdk.Coin) string { return HumanReadable(q) },
	})

	ta.GraphQLClient.RegisterQueryResolver("invites", ta.invitesResolver)
	ta.GraphQLClient.RegisterObjectResolver("Invite", db.Invite{}, map[string]interface{}{
		"id": func(_ context.Context, i db.Invite) int64 { return i.ID },
		"creator": func(ctx context.Context, q db.Invite) *AppAccount {
			return ta.appAccountResolver(ctx, queryByAddress{ID: q.Creator})
		},
		"friend": func(ctx context.Context, i db.Invite) *AppAccount {
			friend, err := ta.DBClient.UserByEmail(i.FriendEmail)
			if err != nil || friend == nil {
				return nil
			}
			return ta.appAccountResolver(ctx, queryByAddress{ID: friend.Address})
		},
		"createdAt": func(_ context.Context, q db.Invite) time.Time { return q.CreatedAt },
	})

	ta.GraphQLClient.RegisterObjectResolver("URL", url.URL{}, map[string]interface{}{
		"url": func(_ context.Context, q url.URL) string { return q.String() },
	})

	ta.GraphQLClient.RegisterQueryResolver("referredAppAccounts", ta.referredAppAccountsResolver)

	ta.GraphQLClient.RegisterQueryResolver("appAccount", ta.appAccountResolver)
	ta.GraphQLClient.RegisterObjectResolver("AppAccount", AppAccount{}, map[string]interface{}{
		"id": func(_ context.Context, q AppAccount) string { return q.Address },
		"availableBalance": func(_ context.Context, q AppAccount) sdk.Coin {
			return sdk.NewCoin(app.StakeDenom, q.Coins.AmountOf(app.StakeDenom))
		},
		"totalClaims": func(ctx context.Context, q AppAccount) int {
			return len(ta.appAccountClaimsCreatedResolver(ctx, queryByAddress{ID: q.Address}))
		},
		"totalArguments": func(ctx context.Context, q AppAccount) int {
			return len(ta.appAccountArgumentsResolver(ctx, queryByAddress{ID: q.Address}))
		},
		"totalAgrees": func(ctx context.Context, q AppAccount) int {
			return len(ta.agreesResolver(ctx, queryByAddress{ID: q.Address}))
		},
		"totalAgreesReceived": func(ctx context.Context, q AppAccount) int64 {
			return ta.agreesReceivedResolver(ctx, q.Address)
		},
		"earnedBalance": func(ctx context.Context, q AppAccount) sdk.Coin {
			return ta.earnedBalanceResolver(ctx, queryByAddress{ID: q.Address})
		},
		"earnedStake": func(ctx context.Context, q AppAccount) []EarnedCoin {
			return ta.earnedStakeResolver(ctx, queryByAddress{ID: q.Address})
		},
		"pendingBalance": func(ctx context.Context, q AppAccount) sdk.Coin {
			return ta.pendingBalanceResolver(ctx, queryByAddress{ID: q.Address})
		},
		"pendingStake": func(ctx context.Context, q AppAccount) []EarnedCoin {
			return ta.pendingStakeResolver(ctx, queryByAddress{ID: q.Address})
		},
		"userProfile": func(ctx context.Context, q AppAccount) *db.UserProfile {
			return ta.userProfileResolver(ctx, q.Address)
		},
		"userJourney": func(ctx context.Context, q AppAccount) []db.UserJourneyStep {
			user := ta.userResolver(ctx, q.Address)
			if user == nil {
				return []db.UserJourneyStep{}
			}

			return user.Meta.Journey
		},
		// deprecated, use "userProfile" instead
		"twitterProfile": func(ctx context.Context, q AppAccount) db.TwitterProfile {
			return ta.twitterProfileResolver(ctx, q.Address)
		},
	})

	ta.GraphQLClient.RegisterObjectResolver("TwitterProfile", db.TwitterProfile{}, map[string]interface{}{
		"id": func(_ context.Context, q db.TwitterProfile) string { return string(q.ID) },
		"avatarURI": func(_ context.Context, q db.TwitterProfile) string {
			largeURI := strings.Replace(q.AvatarURI, "_bigger", "_200x200", 1)
			return strings.Replace(largeURI, "http://", "https://", 1)
		},
	})

	ta.GraphQLClient.RegisterObjectResolver("User", db.UserProfile{}, map[string]interface{}{
		"avatarURL": func(_ context.Context, q db.UserProfile) string {
			largeURI := strings.Replace(q.AvatarURL, "_bigger", "_200x200", 1)
			return strings.Replace(largeURI, "http://", "https://", 1)
		},
	})

	ta.GraphQLClient.RegisterObjectResolver("EarnedCoin", EarnedCoin{}, map[string]interface{}{
		"community": func(ctx context.Context, q EarnedCoin) *community.Community {
			return ta.communityResolver(ctx, queryByCommunityID{CommunityID: q.CommunityID})
		},
	})

	ta.GraphQLClient.RegisterQueryResolver("appAccountCommunityEarnings", ta.appAccountCommunityEarningsResolver)
	ta.GraphQLClient.RegisterObjectResolver("AppAccountCommunityEarnings", appAccountCommunityEarning{}, map[string]interface{}{
		"id": func(_ context.Context, q appAccountCommunityEarning) string { return q.CommunityID },
		"community": func(ctx context.Context, q appAccountCommunityEarning) *community.Community {
			return ta.communityResolver(ctx, queryByCommunityID{CommunityID: q.CommunityID})
		},
	})

	ta.GraphQLClient.RegisterQueryResolver("appAccountEarnings", ta.appAccountEarningsResolver)

	ta.GraphQLClient.RegisterQueryResolver("leaderboard", ta.leaderboardResolver)
	ta.GraphQLClient.RegisterObjectResolver("LeaderboardTopUser", db.LeaderboardTopUser{}, map[string]interface{}{
		"account": func(ctx context.Context, t db.LeaderboardTopUser) *AppAccount {
			return ta.appAccountResolver(ctx, queryByAddress{ID: t.Address})
		},
		"earned": func(ctx context.Context, t db.LeaderboardTopUser) sdk.Coin {
			return sdk.NewInt64Coin(app.StakeDenom, t.Earned)
		},
	})

	ta.GraphQLClient.RegisterQueryResolver("communities", ta.communitiesResolver)
	ta.GraphQLClient.RegisterQueryResolver("community", ta.communityResolver)
	ta.GraphQLClient.RegisterObjectResolver("Community", community.Community{}, map[string]interface{}{
		"id": func(_ context.Context, q community.Community) string { return q.ID },
		"name": func(_ context.Context, q community.Community) string {
			if contains(ta.APIContext.Config.Community.BetaCommunities, q.ID) {
				return q.Name + " (Beta)"
			}
			return q.Name
		},
		"iconImage": ta.communityIconImageResolver,
		"heroImage": func(_ context.Context, q community.Community) string {
			return joinPath(ta.APIContext.Config.App.S3AssetsURL, fmt.Sprintf("communities/%s_hero.jpg", q.ID))
		},
		"following": func(ctx context.Context, q community.Community) bool {
			return ta.followsCommunity(ctx, queryByCommunityID{CommunityID: q.ID})
		},
	})

	ta.GraphQLClient.RegisterPaginatedQueryResolverWithFilter("claims", ta.claimsResolver, map[string]interface{}{
		"body": func(_ context.Context, q claim.Claim) string { return q.Body },
	})
	ta.GraphQLClient.RegisterPaginatedObjectResolver("claims", "iD", claim.Claim{}, map[string]interface{}{
		"id": func(_ context.Context, q claim.Claim) uint64 { return q.ID },
		"community": func(ctx context.Context, q claim.Claim) *community.Community {
			return ta.communityResolver(ctx, queryByCommunityID{CommunityID: q.CommunityID})
		},
		"source": func(ctx context.Context, q claim.Claim) string { return q.Source.String() },
		"image":  ta.claimImageResolver,
		"video":  ta.claimVideoResolver,
		"argumentCount": func(ctx context.Context, q claim.Claim) int {
			return len(ta.claimArgumentsResolver(ctx, queryClaimArgumentParams{ClaimID: q.ID}))
		},
		"topArgument": ta.topArgumentResolver,
		"arguments": func(ctx context.Context, q claim.Claim, a queryClaimArgumentParams) []staking.Argument {
			return ta.claimArgumentsResolver(ctx, queryClaimArgumentParams{ClaimID: q.ID, Address: a.Address, Filter: a.Filter})
		},
		"participants":      ta.claimParticipantsResolver,
		"participantsCount": func(ctx context.Context, q claim.Claim) int { return len(ta.claimParticipantsResolver(ctx, q)) },
		"comments": func(ctx context.Context, q claim.Claim) []db.Comment {
			return ta.commentsResolver(ctx, queryCommentsParams{ClaimID: &q.ID})
		},
		"creator": func(ctx context.Context, q claim.Claim) *AppAccount {
			return ta.appAccountResolver(ctx, queryByAddress{ID: q.Creator.String()})
		},
		"commentCount": func(ctx context.Context, q claim.Claim) int {
			return len(ta.commentsResolver(ctx, queryCommentsParams{ClaimID: &q.ID}))
		},

		// deprecated
		"sourceUrlPreview": ta.claimImageResolver,
		"sourceImage":      ta.claimImageResolver,
	})
	ta.GraphQLClient.RegisterQueryResolver("claim", ta.claimResolver)
	ta.GraphQLClient.RegisterQueryResolver("claimOfTheDay", ta.claimOfTheDayResolver)

	ta.GraphQLClient.RegisterQueryResolver("claimArgument", ta.claimArgumentResolver)
	ta.GraphQLClient.RegisterQueryResolver("claimArguments", ta.claimArgumentsResolver)
	ta.GraphQLClient.RegisterObjectResolver("ClaimArgument", staking.Argument{}, map[string]interface{}{
		"id": func(_ context.Context, q staking.Argument) uint64 { return q.ID },
		"body": func(_ context.Context, q staking.Argument, args struct {
			Raw bool `graphql:",optional"`
		}) string {
			if args.Raw {
				return q.Body
			}
			body, err := ta.DBClient.TranslateToUsersMentions(q.Body)
			if err != nil {
				return q.Body
			}
			return body
		},
		"claimId":     func(_ context.Context, q staking.Argument) uint64 { return q.ClaimID },
		"vote":        func(_ context.Context, q staking.Argument) bool { return q.StakeType == staking.StakeBacking },
		"createdTime": func(_ context.Context, q staking.Argument) string { return q.CreatedTime.String() },
		"editedTime":  func(_ context.Context, q staking.Argument) string { return q.EditedTime.String() },
		"edited":      func(_ context.Context, q staking.Argument) bool { return q.Edited },
		"creator": func(ctx context.Context, q staking.Argument) *AppAccount {
			return ta.appAccountResolver(ctx, queryByAddress{ID: q.Creator.String()})
		},
		"appAccountStake": ta.appAccountStakeResolver,
		"appAccountSlash": ta.appAccountSlashResolver,
		"stakers":         ta.claimArgumentUpvoteStakersResolver,
		"claim": func(ctx context.Context, q staking.Argument) *claim.Claim {
			claim := ta.claimResolver(ctx, queryByClaimID{ID: q.ClaimID})
			return &claim
		},
		"communityId": func(ctx context.Context, q staking.Argument) string {
			return q.CommunityID
		},
	})

	ta.GraphQLClient.RegisterPaginatedQueryResolver("comments", ta.commentsResolver)
	ta.GraphQLClient.RegisterPaginatedObjectResolver("Comment", "iD", db.Comment{}, map[string]interface{}{
		"id":         func(_ context.Context, q db.Comment) int64 { return q.ID },
		"parentId":   func(_ context.Context, q db.Comment) int64 { return q.ParentID },
		"claimId":    func(_ context.Context, q db.Comment) int64 { return q.ClaimID },
		"argumentId": func(_ context.Context, q db.Comment) int64 { return q.ArgumentID },
		"elementId":  func(_ context.Context, q db.Comment) int64 { return q.ElementID },
		"body":       func(_ context.Context, q db.Comment) string { return q.Body },
		"creator": func(ctx context.Context, q db.Comment) *AppAccount {
			return ta.appAccountResolver(ctx, queryByAddress{ID: q.Creator})
		},
		"createdAt": func(_ context.Context, q db.Comment) time.Time { return q.CreatedAt },
	})

	ta.GraphQLClient.RegisterQueryResolver("claimQuestions", ta.claimQuestionsResolver)
	ta.GraphQLClient.RegisterObjectResolver("Question", db.Question{}, map[string]interface{}{
		"id":      func(_ context.Context, q db.Question) int64 { return q.ID },
		"claimId": func(_ context.Context, q db.Question) int64 { return q.ClaimID },
		"body":    func(_ context.Context, q db.Question) string { return q.Body },
		"creator": func(ctx context.Context, q db.Question) *AppAccount {
			return ta.appAccountResolver(ctx, queryByAddress{ID: q.Creator})
		},
		"createdAt": func(_ context.Context, q db.Question) time.Time { return q.CreatedAt },
	})

	ta.GraphQLClient.RegisterObjectResolver("Stake", staking.Stake{}, map[string]interface{}{
		"id": func(_ context.Context, q staking.Stake) uint64 { return q.ID },
		"creator": func(ctx context.Context, q staking.Stake) *AppAccount {
			return ta.appAccountResolver(ctx, queryByAddress{ID: q.Creator.String()})
		},
		"stake": func(ctx context.Context, q staking.Stake) sdk.Coin { return q.Amount },
	})

	ta.GraphQLClient.RegisterQueryResolver("slashes", ta.slashesResolver)

	ta.GraphQLClient.RegisterObjectResolver("Slash", slashing.Slash{}, map[string]interface{}{
		"id":         func(_ context.Context, q slashing.Slash) uint64 { return q.ID },
		"argumentId": func(_ context.Context, q slashing.Slash) uint64 { return q.ArgumentID },
		"argument": func(ctx context.Context, q slashing.Slash) *staking.Argument {
			return ta.claimArgumentResolver(ctx, queryByArgumentID{ID: q.ArgumentID})
		},
		"creator": func(ctx context.Context, q slashing.Slash) *AppAccount {
			return ta.appAccountResolver(ctx, queryByAddress{ID: q.Creator.String()})
		},
	})

	ta.GraphQLClient.RegisterPaginatedQueryResolver("transactions", ta.appAccountTransactionsResolver)
	ta.GraphQLClient.RegisterPaginatedObjectResolver("Transaction", "iD", bank.Transaction{}, map[string]interface{}{
		"id":        func(_ context.Context, q bank.Transaction) uint64 { return q.ID },
		"reference": ta.transactionReferenceResolver,
		"amount": func(_ context.Context, q bank.Transaction) sdk.Coin {
			amount := q.Amount.Amount
			if q.Type.AllowedForDeduction() {
				amount = amount.Neg()
			}
			return sdk.Coin{
				Amount: amount,
				Denom:  q.Amount.Denom,
			}
		},
	})

	ta.GraphQLClient.RegisterPaginatedQueryResolver("appAccountClaimsCreated", ta.appAccountClaimsCreatedResolver)
	ta.GraphQLClient.RegisterPaginatedQueryResolver("appAccountClaimsWithArguments", ta.appAccountClaimsWithArgumentsResolver)
	ta.GraphQLClient.RegisterPaginatedQueryResolver("appAccountClaimsWithAgrees", ta.appAccountClaimsWithAgreesResolver)

	ta.GraphQLClient.RegisterQueryResolver("settings", ta.settingsResolver)
	ta.GraphQLClient.RegisterObjectResolver("Settings", Settings{}, map[string]interface{}{})

	ta.GraphQLClient.RegisterPaginatedQueryResolver("notifications", ta.notificationsResolver)
	ta.GraphQLClient.RegisterObjectResolver("NotificationMeta", db.NotificationMeta{}, map[string]interface{}{})
	ta.GraphQLClient.RegisterPaginatedObjectResolver("NotificationEvent", "iD", db.NotificationEvent{}, map[string]interface{}{
		"id": func(_ context.Context, q db.NotificationEvent) int64 { return q.ID },
		"userId": func(_ context.Context, q db.NotificationEvent) int64 {
			if q.SenderProfile != nil {
				return q.SenderProfileID
			}
			return q.UserProfileID
		},
		"title": func(_ context.Context, q db.NotificationEvent) string {
			return q.Type.String()
		},
		"senderProfile": func(ctx context.Context, q db.NotificationEvent) *AppAccount {
			if q.SenderProfile != nil {
				sender, err := ta.DBClient.UserByID(q.SenderProfileID)
				if err != nil {
					return nil
				}
				return ta.appAccountResolver(ctx, queryByAddress{ID: sender.Address})
			}
			return nil
		},
		"createdTime": func(_ context.Context, q db.NotificationEvent) time.Time {
			return q.Timestamp
		},
		"body": func(_ context.Context, q db.NotificationEvent) string {
			return q.Message
		},
		"typeId": func(_ context.Context, q db.NotificationEvent) int64 { return q.TypeID },
		"image": func(_ context.Context, q db.NotificationEvent) string {
			icon, ok := NotificationIcons[q.Type]
			if ok {
				return joinPath(ta.APIContext.Config.App.S3AssetsURL, path.Join("notifications", icon))
			}
			if q.SenderProfile != nil {
				return strings.Replace(q.SenderProfile.AvatarURL, "http://", "https://", 1)
			}
			return strings.Replace(q.UserProfile.AvatarURL, "http://", "https://", 1)
		},
		"meta": func(_ context.Context, q db.NotificationEvent) db.NotificationMeta {
			return q.Meta
		},
	})

	ta.GraphQLClient.RegisterQueryResolver("unreadNotificationsCount", ta.unreadNotificationsCountResolver)
	ta.GraphQLClient.RegisterQueryResolver("unseenNotificationsCount", ta.unseenNotificationsCountResolver)

	ta.GraphQLClient.BuildSchema()
}
