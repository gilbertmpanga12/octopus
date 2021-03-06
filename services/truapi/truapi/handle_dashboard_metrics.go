package truapi

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	app "github.com/TruStory/truchain/types"
	"github.com/TruStory/truchain/x/bank/exported"
	"github.com/TruStory/truchain/x/claim"
	"github.com/TruStory/truchain/x/community"
	"github.com/TruStory/truchain/x/staking"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/TruStory/octopus/services/truapi/db"
	"github.com/TruStory/octopus/services/truapi/truapi/render"
)

const metricsVersion = "20190911-01"

type UserCommunityMetrics struct {
	Claims                  int
	Arguments               int
	AgreesGiven             int
	AgreesReceived          int
	Staked                  sdk.Coin
	StakedArgument          sdk.Coin
	StakedAgree             sdk.Coin
	InterestArgumentCreated sdk.Coin
	InterestAgreeReceived   sdk.Coin
	InterestAgreeGiven      sdk.Coin
	CuratorReward           sdk.Coin
	InterestSlashed         sdk.Coin
	StakeSlashed            sdk.Coin
	ClaimsOpened            int64
	UniqueClaimsOpened      int64
	ArgumentsOpened         int64
	UniqueArgumentsOpened   int64
	Replies                 int64
	EarnedCoin              sdk.Coin
	PendingStake            sdk.Coin
}

type UserMetrics struct {
	Balance          sdk.Coin
	CommunityMetrics map[string]*UserCommunityMetrics
}

type Metrics struct {
	UserMetrics map[string]*UserMetrics
}

func (m *Metrics) getUserMetrics(address string) *UserMetrics {
	userMetrics, ok := m.UserMetrics[address]
	if !ok {
		userMetrics = &UserMetrics{CommunityMetrics: make(map[string]*UserCommunityMetrics),
			Balance: sdk.NewInt64Coin(app.StakeDenom, 0)}
		m.UserMetrics[address] = userMetrics
	}
	return userMetrics
}
func (m *Metrics) getUserCommunityMetric(address, communityID string) *UserCommunityMetrics {
	userMetrics := m.getUserMetrics(address)
	ucm, ok := userMetrics.CommunityMetrics[communityID]
	if !ok {
		ucm = &UserCommunityMetrics{
			InterestArgumentCreated: sdk.NewInt64Coin(app.StakeDenom, 0),
			InterestAgreeReceived:   sdk.NewInt64Coin(app.StakeDenom, 0),
			InterestAgreeGiven:      sdk.NewInt64Coin(app.StakeDenom, 0),
			CuratorReward:           sdk.NewInt64Coin(app.StakeDenom, 0),
			InterestSlashed:         sdk.NewInt64Coin(app.StakeDenom, 0),
			StakeSlashed:            sdk.NewInt64Coin(app.StakeDenom, 0),
			EarnedCoin:              sdk.NewInt64Coin(app.StakeDenom, 0),
			Staked:                  sdk.NewInt64Coin(app.StakeDenom, 0),
			StakedArgument:          sdk.NewInt64Coin(app.StakeDenom, 0),
			StakedAgree:             sdk.NewInt64Coin(app.StakeDenom, 0),
			PendingStake:            sdk.NewInt64Coin(app.StakeDenom, 0),
		}
		userMetrics.CommunityMetrics[communityID] = ucm
	}
	return ucm
}

func (ta *TruAPI) getClaimArguments(claimID uint64) ([]staking.Argument, error) {
	queryRoute := path.Join(staking.ModuleName, staking.QueryClaimArguments)
	res, err := ta.Query(queryRoute, staking.QueryClaimArgumentsParams{ClaimID: claimID}, staking.ModuleCodec)
	if err != nil {
		return nil, err
	}

	arguments := make([]staking.Argument, 0)
	err = staking.ModuleCodec.UnmarshalJSON(res, &arguments)
	if err != nil {
		return nil, err
	}
	return arguments, nil
}

func notExpiredAt(date, created, end time.Time) bool {
	betaReleaseDate, err := time.Parse("2006-01-02", "2019-07-11")
	if err != nil {
		return false
	}
	betaReleaseDate = betaReleaseDate.UTC()

	// return always as expired any stake created before beta.
	if created.Before(betaReleaseDate) {
		return false
	}
	if date.Before(created) {
		return false
	}
	if date.After(end) {
		return false
	}
	if !created.Before(end) {
		return false
	}
	return true
}

func (ta *TruAPI) HandleUsersMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("x-metrics-version", metricsVersion)
	jobTime := time.Now().UTC().Format("200601021504")
	err := r.ParseForm()
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	date := r.FormValue("date")
	if date == "" {
		render.Error(w, r, "provide a valid date", http.StatusBadRequest)
		return
	}

	beforeDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get all claims
	claims := make([]claim.Claim, 0)
	result, err := ta.Query(
		path.Join(claim.QuerierRoute, claim.QueryClaimsBeforeTime),
		claim.QueryClaimsTimeParams{CreatedTime: beforeDate},
		claim.ModuleCodec,
	)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	err = claim.ModuleCodec.UnmarshalJSON(result, &claims)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}

	// For each user, get the available stake calculated.
	users := make([]db.User, 0)
	err = ta.DBClient.FindAll(&users)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
	}

	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
	}
	chainMetrics := &Metrics{UserMetrics: make(map[string]*UserMetrics)}

	for _, claim := range claims {
		if !claim.CreatedTime.Before(beforeDate) {
			continue
		}
		argumentIDCreator := make(map[uint64]string)
		ucm := chainMetrics.getUserCommunityMetric(claim.Creator.String(), claim.CommunityID)
		ucm.Claims++
		arguments, err := ta.getClaimArguments(claim.ID)
		if err != nil {
			render.Error(w, r, err.Error(), http.StatusInternalServerError)
		}
		for _, argument := range arguments {
			if !argument.CreatedTime.Before(beforeDate) {
				continue
			}
			acm := chainMetrics.getUserCommunityMetric(argument.Creator.String(), claim.CommunityID)
			acm.Arguments++
			argumentIDCreator[argument.ID] = argument.Creator.String()
		}
		stakes := ta.claimStakesResolver(r.Context(), claim)
		for _, stake := range stakes {
			if !stake.CreatedTime.Before(beforeDate) {
				continue
			}
			scm := chainMetrics.getUserCommunityMetric(stake.Creator.String(), claim.CommunityID)
			if !stake.Expired || notExpiredAt(beforeDate, stake.CreatedTime, stake.EndTime) {
				scm.PendingStake = scm.PendingStake.Add(stake.Amount)
			}
			if stake.Type == staking.StakeUpvote {
				scm.StakedAgree = scm.StakedAgree.Add(stake.Amount)
				chainMetrics.getUserCommunityMetric(argumentIDCreator[stake.ArgumentID], stake.CommunityID).AgreesReceived++
				scm.AgreesGiven++
			}

			if stake.Type != staking.StakeUpvote {
				scm.StakedArgument = scm.StakedArgument.Add(stake.Amount)
			}
			scm.Staked = scm.Staked.Add(stake.Amount)

		}
	}
	// Get all communities
	queryRoute := path.Join(community.QuerierRoute, community.QueryCommunities)
	res, err := ta.Query(queryRoute, struct{}{}, community.ModuleCodec)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
	}

	communities := make([]community.Community, 0)
	err = community.ModuleCodec.UnmarshalJSON(res, &communities)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
	}
	if len(communities) == 0 {
		render.Error(w, r, "no communities found", http.StatusInternalServerError)
		return
	}
	trackedTransactions := []exported.TransactionType{
		exported.TransactionBacking,
		exported.TransactionChallenge,
		exported.TransactionCuratorReward,
		exported.TransactionInterestArgumentCreation,
		exported.TransactionInterestUpvoteReceived,
		exported.TransactionInterestUpvoteGiven,
		// slashing
		exported.TransactionInterestArgumentCreationSlashed,
		exported.TransactionInterestUpvoteReceivedSlashed,
		exported.TransactionInterestUpvoteGivenSlashed,
		exported.TransactionStakeCreatorSlashed,
		exported.TransactionStakeCuratorSlashed,
	}
	w.Header().Add("Content-Type", "text/csv")
	csvw := csv.NewWriter(w)
	header := []string{"job_date_time", "date", "address", "username", "balance",
		"community", "community_name", "stake_earned",
		"claims_created", "claims_opened", "unique_claims_opened",
		"arguments_created", "agrees_received", "agrees_given",
		"staked", "staked_arguments", "staked_agrees",
		"interest_argument_creation", "interest_agree_received", "interest_agree_given", "reward_not_helpful",
		"interest_slashed", "stake_slashed", "pending_stake",
		"replies",
		"arguments_opened", "unique_arguments_opened",
	}
	err = csvw.Write(header)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
	}
	openedClaims, err := ta.DBClient.OpenedClaimsSummary(beforeDate)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
	}
	for _, userOpenedClaims := range openedClaims {
		userMetrics := chainMetrics.getUserCommunityMetric(userOpenedClaims.Address, userOpenedClaims.CommunityID)
		userMetrics.ClaimsOpened = userOpenedClaims.OpenedClaims
		userMetrics.UniqueClaimsOpened = userOpenedClaims.UniqueOpenedClaims
	}

	openedArguments, err := ta.DBClient.OpenedArgumentsSummary(beforeDate)
	if err != nil {
		fmt.Println(err)
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, userOpenedArguments := range openedArguments {
		userMetrics := chainMetrics.getUserCommunityMetric(userOpenedArguments.Address, userOpenedArguments.CommunityID)
		userMetrics.ArgumentsOpened = userOpenedArguments.OpenedArguments
		userMetrics.UniqueArgumentsOpened = userOpenedArguments.UniqueOpenedArguments
	}

	replies, err := ta.DBClient.UserRepliesStats(beforeDate)
	if err != nil {
		fmt.Println(err)
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, userReplies := range replies {
		userMetrics := chainMetrics.getUserCommunityMetric(userReplies.Address, userReplies.CommunityID)
		userMetrics.Replies = userReplies.Replies
	}
	for _, user := range users {
		if user.Address == "" || !user.CreatedAt.Before(beforeDate) {
			continue
		}
		transactions := ta.appAccountTransactionsResolver(r.Context(), queryByAddress{ID: user.Address})
		balance := sdk.NewInt64Coin(app.StakeDenom, 0)
		for _, transaction := range transactions {
			if !transaction.CreatedTime.Before(beforeDate) {
				continue
			}

			if transaction.Type.AllowedForDeduction() {
				transaction.Amount.Amount = transaction.Amount.Amount.Neg()
			}
			balance = balance.Add(transaction.Amount)
			if !transaction.Type.OneOf(trackedTransactions) {
				continue
			}
			if transaction.CommunityID == "" {
				render.Error(w, r,
					fmt.Sprintf("transaction %s [%d] must contain community id",
						transaction.Type.String(), transaction.ID),
					http.StatusInternalServerError)
				return
			}

			ucm := chainMetrics.getUserCommunityMetric(user.Address, transaction.CommunityID)
			switch transaction.Type {
			case exported.TransactionInterestArgumentCreation:
				ucm.InterestArgumentCreated = ucm.InterestArgumentCreated.Add(transaction.Amount)
				ucm.EarnedCoin = sdk.NewCoin(transaction.CommunityID, ucm.EarnedCoin.Amount.Add(transaction.Amount.Amount))
			case exported.TransactionInterestUpvoteReceived:
				ucm.InterestAgreeReceived = ucm.InterestAgreeReceived.Add(transaction.Amount)
				ucm.EarnedCoin = sdk.NewCoin(transaction.CommunityID, ucm.EarnedCoin.Amount.Add(transaction.Amount.Amount))
			case exported.TransactionInterestUpvoteGiven:
				ucm.InterestAgreeGiven = ucm.InterestAgreeGiven.Add(transaction.Amount)
				ucm.EarnedCoin = sdk.NewCoin(transaction.CommunityID, ucm.EarnedCoin.Amount.Add(transaction.Amount.Amount))
			case exported.TransactionCuratorReward:
				ucm.CuratorReward = ucm.CuratorReward.Add(transaction.Amount)
			case exported.TransactionInterestArgumentCreationSlashed:
				ucm.InterestSlashed = ucm.InterestSlashed.Add(transaction.Amount)
				ucm.EarnedCoin = sdk.NewCoin(transaction.CommunityID, ucm.EarnedCoin.Amount.Sub(transaction.Amount.Amount))
			case exported.TransactionInterestUpvoteReceivedSlashed:
				ucm.InterestSlashed = ucm.InterestSlashed.Add(transaction.Amount)
				ucm.EarnedCoin = sdk.NewCoin(transaction.CommunityID, ucm.EarnedCoin.Amount.Sub(transaction.Amount.Amount))
			case exported.TransactionInterestUpvoteGivenSlashed:
				ucm.InterestSlashed = ucm.InterestSlashed.Add(transaction.Amount)
				ucm.EarnedCoin = sdk.NewCoin(transaction.CommunityID, ucm.EarnedCoin.Amount.Sub(transaction.Amount.Amount))
			case exported.TransactionStakeCreatorSlashed:
				ucm.StakeSlashed = ucm.StakeSlashed.Add(transaction.Amount)
			case exported.TransactionStakeCuratorSlashed:
				ucm.StakeSlashed = ucm.StakeSlashed.Add(transaction.Amount)
			}

		}
		// "job_time", "date", "address", "username", "balance"
		rowStart := []string{jobTime, beforeDate.Format(time.RFC3339Nano), user.Address, user.Username, balance.Amount.String()}

		for _, community := range communities {
			// 	"community", "community_name"
			record := append(rowStart, community.ID)
			record = append(record, community.Name)
			m := chainMetrics.getUserCommunityMetric(user.Address, community.ID)
			// "stake_earned"
			record = append(record, m.EarnedCoin.Amount.String())
			// "claims_created", "claims_opened", "unique_claims_opened",
			record = append(record, fmt.Sprintf("%d", m.Claims))
			record = append(record, fmt.Sprintf("%d", m.ClaimsOpened))
			record = append(record, fmt.Sprintf("%d", m.UniqueClaimsOpened))
			// "arguments_created", "agrees_received", "agrees_given",
			record = append(record, fmt.Sprintf("%d", m.Arguments))
			record = append(record, fmt.Sprintf("%d", m.AgreesReceived))
			record = append(record, fmt.Sprintf("%d", m.AgreesGiven))
			// "staked", "staked_argument", "staked_agree"
			record = append(record, m.Staked.Amount.String())
			record = append(record, m.StakedArgument.Amount.String())
			record = append(record, m.StakedAgree.Amount.String())
			// "interest_argument_creation", "interest_agree_received", "interest_agree_given", "reward_not_helpful",
			record = append(record, m.InterestArgumentCreated.Amount.String())
			record = append(record, m.InterestAgreeReceived.Amount.String())
			record = append(record, m.InterestAgreeGiven.Amount.String())
			record = append(record, m.CuratorReward.Amount.String())
			// "interest_slashed", "stake_slashed", "at_stake"
			record = append(record, m.InterestSlashed.Amount.String())
			record = append(record, m.StakeSlashed.Amount.String())
			record = append(record, m.PendingStake.Amount.String())
			// "replies"
			record = append(record, fmt.Sprintf("%d", m.Replies))
			// "arguments_opened", "unique_arguments_opened"
			record = append(record, fmt.Sprintf("%d", m.ArgumentsOpened))
			record = append(record, fmt.Sprintf("%d", m.UniqueArgumentsOpened))
			err = csvw.Write(record)
			if err != nil {
				render.Error(w, r, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		csvw.Flush()
	}

}

// HandleClaimMetrics returns metrics for claims
func (ta *TruAPI) HandleClaimMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("x-metrics-version", metricsVersion)
	jobTime := time.Now().UTC().Format("200601021504")
	err := r.ParseForm()
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	date := r.FormValue("date")
	if date == "" {
		render.Error(w, r, "provide a valid date", http.StatusBadRequest)
		return
	}

	beforeDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	// Get all claims
	claims := make([]claim.Claim, 0)
	result, err := ta.Query(
		path.Join(claim.QuerierRoute, claim.QueryClaimsBeforeTime),
		claim.QueryClaimsTimeParams{CreatedTime: beforeDate},
		claim.ModuleCodec,
	)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	err = claim.ModuleCodec.UnmarshalJSON(result, &claims)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	claimViewsStats, err := ta.DBClient.ClaimViewsStats(beforeDate)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	claimRepliesStats, err := ta.DBClient.ClaimRepliesStats(beforeDate)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Add("Content-Type", "text/csv")
	csvw := csv.NewWriter(w)
	header := []string{
		"job_date_time", "date", "created_date", "flagged", "id", "community_id", "claim_name",
		"arguments_created", "agrees_given",
		//staked
		"staked",
		// staked backed
		"staked_backed", "staked_argument_backed", "staked_agree_backed",
		// staked challenge
		"staked_challenged", "staked_argument_challenged", "staked_agree_challenged",
		// claim views
		"user_views", "unique_user_views", "anon_views", "unique_anon_views",
		// argument views
		"user_arguments_views", "unique_user_arguments_views", "anon_arguments_views", "unique_anon_arguments_views",
		// comments
		"replies",
		"last_activiy_argument",
		"last_activity_agree",
	}
	err = csvw.Write(header)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
	}
	// claim stats
	claimViewsStatsMappings := make(map[uint64]int)
	for idx, c := range claimViewsStats {
		claimViewsStatsMappings[uint64(c.ClaimID)] = idx
	}
	getClaimViewsStats := func(claimID uint64) db.ClaimViewsStats {
		index, ok := claimViewsStatsMappings[claimID]
		if !ok {
			return db.ClaimViewsStats{}
		}
		return claimViewsStats[index]
	}
	claimRepliesStatsMappings := make(map[uint64]int)
	for idx, c := range claimRepliesStats {
		claimRepliesStatsMappings[uint64(c.ClaimID)] = idx
	}
	getClaimRepliesStats := func(claimID uint64) db.ClaimRepliesStats {
		index, ok := claimRepliesStatsMappings[claimID]
		if !ok {
			return db.ClaimRepliesStats{}
		}
		return claimRepliesStats[index]
	}
	flaggedClaimsIDs, err := ta.DBClient.FlaggedStoriesIDs(ta.APIContext.Config.Flag.Admin, ta.APIContext.Config.Flag.Limit)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
	}
	flaggedClaimsMappings := make(map[uint64]int)
	for _, c := range flaggedClaimsIDs {
		flaggedClaimsMappings[uint64(c)] = 1
	}
	for _, claim := range claims {
		if !claim.CreatedTime.Before(beforeDate) {
			continue
		}
		totalArguments := 0
		agreesGiven := 0
		totalBacked := sdk.NewInt(0)
		totalBackedAgree := sdk.NewInt(0)
		totalBackedArgument := sdk.NewInt(0)
		totalChallenged := sdk.NewInt(0)
		totalChallengedAgree := sdk.NewInt(0)
		totalChallengedArgument := sdk.NewInt(0)
		var lastActivityArgument time.Time
		var lastActivityAgree time.Time
		mapArguments := make(map[uint64]int)
		arguments, err := ta.getClaimArguments(claim.ID)

		for idx, argument := range arguments {
			mapArguments[argument.ID] = idx
			if !argument.CreatedTime.Before(beforeDate) {
				continue
			}
			if lastActivityArgument.Before(argument.CreatedTime) {
				lastActivityArgument = argument.CreatedTime
			}
			totalArguments++
		}
		if err != nil {
			render.Error(w, r, err.Error(), http.StatusInternalServerError)
		}
		stakes := ta.claimStakesResolver(r.Context(), claim)
		for _, stake := range stakes {
			if !stake.CreatedTime.Before(beforeDate) {
				continue
			}
			i, ok := mapArguments[stake.ArgumentID]
			if !ok {
				render.Error(w, r, fmt.Sprintf("unable to find argument with id %d", stake.ArgumentID), http.StatusInternalServerError)
			}
			a := arguments[i]
			if stake.Type == staking.StakeUpvote && lastActivityAgree.Before(stake.CreatedTime) {
				lastActivityAgree = stake.CreatedTime
			}
			if a.StakeType == staking.StakeBacking && stake.Type == staking.StakeUpvote {
				totalBacked = totalBacked.Add(stake.Amount.Amount)
				totalBackedAgree = totalBackedAgree.Add(stake.Amount.Amount)
				agreesGiven++
				continue
			}
			if a.StakeType == staking.StakeChallenge && stake.Type == staking.StakeUpvote {
				totalChallenged = totalChallenged.Add(stake.Amount.Amount)
				totalChallengedAgree = totalChallengedAgree.Add(stake.Amount.Amount)
				agreesGiven++
				continue
			}

			if a.StakeType == staking.StakeBacking {
				totalBacked = totalBacked.Add(stake.Amount.Amount)
				totalBackedArgument = totalBackedArgument.Add(stake.Amount.Amount)
			}
			if a.StakeType == staking.StakeChallenge {
				totalChallenged = totalChallenged.Add(stake.Amount.Amount)
				totalChallengedArgument = totalChallengedArgument.Add(stake.Amount.Amount)
			}

		}
		body := strings.ReplaceAll(claim.Body, "\n", " ")
		viewsStats := getClaimViewsStats(claim.ID)
		repliesStats := getClaimRepliesStats(claim.ID)
		lastActivityArgumentDateString := ""
		if !lastActivityArgument.IsZero() {
			lastActivityArgumentDateString = lastActivityArgument.Format(time.RFC3339Nano)
		}
		lastActivityAgreeDateString := ""
		if !lastActivityAgree.IsZero() {
			lastActivityAgreeDateString = lastActivityAgree.Format(time.RFC3339Nano)
		}
		row := []string{jobTime,
			beforeDate.Format(time.RFC3339Nano),
			claim.CreatedTime.Format(time.RFC3339Nano),
			fmt.Sprintf("%d", flaggedClaimsMappings[claim.ID]),
			fmt.Sprintf("%d", claim.ID),
			claim.CommunityID,
			strings.TrimSpace(body),
			fmt.Sprintf("%d", totalArguments),
			fmt.Sprintf("%d", agreesGiven),
			totalBacked.Add(totalChallenged).String(),
			totalBacked.String(),
			totalBackedArgument.String(),
			totalBackedAgree.String(),
			totalChallenged.String(),
			totalChallengedArgument.String(),
			totalChallengedAgree.String(),
			fmt.Sprintf("%d", viewsStats.UserViews),
			fmt.Sprintf("%d", viewsStats.UniqueUserViews),
			fmt.Sprintf("%d", viewsStats.AnonViews),
			fmt.Sprintf("%d", viewsStats.UniqueAnonViews),
			fmt.Sprintf("%d", viewsStats.UserArgumentsViews),
			fmt.Sprintf("%d", viewsStats.UniqueUserArgumentsViews),
			fmt.Sprintf("%d", viewsStats.AnonArgumentsViews),
			fmt.Sprintf("%d", viewsStats.UniqueAnonArgumentsViews),
			fmt.Sprintf("%d", repliesStats.Replies),
			lastActivityArgumentDateString,
			lastActivityAgreeDateString,
		}
		if len(header) != len(row) {
			render.Error(w, r, "header and row content mismatch", http.StatusInternalServerError)
			return
		}
		err = csvw.Write(row)
		if err != nil {
			render.Error(w, r, err.Error(), http.StatusInternalServerError)
			return
		}
		csvw.Flush()
	}
}

func (ta *TruAPI) HandleUserClaims(w http.ResponseWriter, r *http.Request) {
	jobTime := time.Now().UTC().Format("200601021504")
	w.Header().Add("Content-Type", "text/csv")
	csvw := csv.NewWriter(w)
	header := []string{
		"job_date_time", "date", "claim_id", "claim", "community", "address", "creation_date", "participants",
	}
	err := csvw.Write(header)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	ctx := ta.createContext(r.Context())
	err = r.ParseForm()
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	date := r.FormValue("date")
	if date == "" {
		render.Error(w, r, "provide a valid date", http.StatusBadRequest)
		return
	}

	targetDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	// Get all claims
	claims := make([]claim.Claim, 0)
	result, err := ta.Query(
		path.Join(claim.QuerierRoute, claim.QueryClaimsBeforeTime),
		claim.QueryClaimsTimeParams{CreatedTime: targetDate},
		claim.ModuleCodec,
	)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	err = claim.ModuleCodec.UnmarshalJSON(result, &claims)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	previousDay := targetDate.Add(-24 * time.Hour)
	for _, claim := range claims {
		if !claim.CreatedTime.Before(targetDate) {
			continue
		}
		participantsTarget := make(map[string]bool)
		participantsPreviousDay := make(map[string]bool)
		stakes := ta.claimStakesResolver(ctx, claim)

		for _, s := range stakes {
			if !s.CreatedTime.Before(targetDate) {
				continue
			}
			participantsTarget[s.Creator.String()] = true
			if s.CreatedTime.Before(previousDay) {
				participantsPreviousDay[s.Creator.String()] = true
			}
		}
		comments, _ := ta.DBClient.CommentsByClaimID(claim.ID)

		for _, c := range comments {
			if !c.CreatedAt.Before(targetDate) {
				continue
			}
			participantsTarget[c.Creator] = true
			if c.CreatedAt.Before(previousDay) {
				participantsPreviousDay[c.Creator] = true
			}
		}
		// "job_date_time", "claim_id", "claim", "community", "address", "creation_date", "participants",
		row := []string{jobTime, targetDate.Format(time.RFC3339Nano), fmt.Sprintf("%d", claim.ID),
			claim.Body, claim.CommunityID, claim.Creator.String(), claim.CreatedTime.Format(time.RFC3339Nano),
			fmt.Sprintf("%d", len(participantsTarget)-len(participantsPreviousDay)),
		}
		err := csvw.Write(row)
		if err != nil {
			render.Error(w, r, err.Error(), http.StatusInternalServerError)
			return
		}
		csvw.Flush()
	}
}

// HandleUserBase returns the user base.
func (ta *TruAPI) HandleUserBase(w http.ResponseWriter, r *http.Request) {
	token := ta.APIContext.Config.Metrics.Secret
	if token == "" || token != r.Header.Get("Metrics-Secret") {
		render.Error(w, r, "Invalid token", http.StatusUnauthorized)
		return
	}

	w.Header().Add("Content-Type", "text/csv")
	csvw := csv.NewWriter(w)
	header := []string{
		"address", "username", "email", "creation_date", "updated_date", "last_login", "user_group",
	}
	err := csvw.Write(header)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	// For each user, get the available stake calculated.
	users := make([]db.User, 0)
	err = ta.DBClient.FindAll(&users)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, user := range users {
		if user.Address == "" {
			continue
		}
		lastLogin := ""
		if user.LastAuthenticatedAt != nil {
			lastLogin = user.LastAuthenticatedAt.Format(time.RFC3339Nano)
		}
		row := []string{
			user.Address,
			user.Username,
			user.Email,
			user.CreatedAt.Format(time.RFC3339Nano),
			user.UpdatedAt.Format(time.RFC3339Nano),
			lastLogin,
			user.UserGroup.String(),
		}
		err := csvw.Write(row)
		if err != nil {
			render.Error(w, r, err.Error(), http.StatusInternalServerError)
			return
		}
		csvw.Flush()
	}
}
