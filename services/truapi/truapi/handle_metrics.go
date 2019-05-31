package truapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	db "github.com/TruStory/octopus/services/truapi/db"
	"github.com/TruStory/octopus/services/truapi/truapi/render"
	app "github.com/TruStory/truchain/types"
	"github.com/TruStory/truchain/x/category"
	"github.com/TruStory/truchain/x/story"
	trubank "github.com/TruStory/truchain/x/trubank"
	"github.com/TruStory/truchain/x/users"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MetricsSummary represents metrics for the platform.
type MetricsSummary struct {
	Users      map[string]*UserMetrics     `json:"users"`
	Categories map[int64]category.Category `json:"-"`
}

func (um *UserMetrics) getMetricsByCategory(categoryID int64) *CategoryMetrics {
	cm, ok := um.CategoryMetrics[categoryID]
	if !ok {
		categoryMetrics := &CategoryMetrics{
			CategoryID: categoryID,
			Metrics: &Metrics{
				InterestEarned:        sdk.NewCoin(app.StakeDenom, sdk.NewInt(0)),
				StakeLost:             sdk.NewCoin(app.StakeDenom, sdk.NewInt(0)),
				StakeEarned:           sdk.NewCoin(app.StakeDenom, sdk.NewInt(0)),
				TotalAmountAtStake:    sdk.NewCoin(app.StakeDenom, sdk.NewInt(0)),
				TotalAmountStaked:     sdk.NewCoin(app.StakeDenom, sdk.NewInt(0)),
				TotalAmountBacked:     sdk.NewCoin(app.StakeDenom, sdk.NewInt(0)),
				TotalAmountChallenged: sdk.NewCoin(app.StakeDenom, sdk.NewInt(0)),
			},
		}
		um.CategoryMetrics[categoryID] = categoryMetrics
		return categoryMetrics
	}
	return cm
}

// GetUserMetrics gets user metrics or initializes one if not in the map.
func (m *MetricsSummary) GetUserMetrics(address string) *UserMetrics {
	userMetrics, ok := m.Users[address]
	if !ok {
		userMetrics = &UserMetrics{
			CategoryMetrics: make(map[int64]*CategoryMetrics),
		}
	}
	m.setUserMetrics(address, userMetrics)
	return userMetrics
}

func (m *MetricsSummary) setUserMetrics(address string, userMetrics *UserMetrics) {
	m.Users[address] = userMetrics
}

// Metrics tracked.
type Metrics struct {
	// Interactions
	TotalClaims               int64 `json:"total_claims"`
	TotalArguments            int64 `json:"total_arguments"`
	TotalEndorsementsReceived int64 `json:"total_endorsements_received"`
	TotalEndorsementsGiven    int64 `json:"total_endorsements_given"`
	TotalBackings             int64 `json:"total_backings"`
	TotalChallenges           int64 `json:"total_challenges"`
	TotalOpenedClaims         int64 `json:"total_opened_claims"`

	// StakeBased Metrics
	TotalAmountStaked     sdk.Coin `json:"total_amount_staked"`
	TotalAmountBacked     sdk.Coin `json:"total_amount_backed"`
	TotalAmountChallenged sdk.Coin `json:"total_amount_challenged"`
	StakeEarned           sdk.Coin `json:"stake_earned"`
	StakeLost             sdk.Coin `json:"stake_lost"`
	TotalAmountAtStake    sdk.Coin `json:"total_amount_at_stake"`
	InterestEarned        sdk.Coin `json:"interest_earned"`
}

// CategoryMetrics summary of metrics by category.
type CategoryMetrics struct {
	CategoryID   int64    `json:"category_id"`
	CategoryName string   `json:"category_name"`
	CredEarned   sdk.Coin `json:"cred_earned"`
	Metrics      *Metrics `json:"metrics"`
}

// UserMetrics a summary of different metrics per user
type UserMetrics struct {
	UserName       string   `json:"username"`
	Balance        sdk.Coin `json:"balance"`
	RunningBalance sdk.Coin `json:"running_balance"`

	// ByCategoryID
	CategoryMetrics map[int64]*CategoryMetrics `json:"category_metrics"`
}

func (um *UserMetrics) increaseArgumentsCount(categoryID int64) {
	m := um.getMetricsByCategory(categoryID).Metrics
	m.TotalArguments = m.TotalArguments + 1
}

func (um *UserMetrics) increaseClaimsCount(categoryID int64) {
	m := um.getMetricsByCategory(categoryID).Metrics
	m.TotalClaims = m.TotalClaims + 1
}

func (um *UserMetrics) increaseEndorsementsGivenCount(categoryID int64) {
	m := um.getMetricsByCategory(categoryID).Metrics
	m.TotalEndorsementsGiven = m.TotalEndorsementsGiven + 1
}

func (um *UserMetrics) increaseEndorsementsReceivedCount(categoryID int64) {
	m := um.getMetricsByCategory(categoryID).Metrics
	m.TotalEndorsementsReceived = m.TotalEndorsementsReceived + 1
}

func (um *UserMetrics) increaseTotalBackings(categoryID int64) {
	m := um.getMetricsByCategory(categoryID).Metrics
	m.TotalBackings = m.TotalBackings + 1
}

func (um *UserMetrics) increaseTotalChallenges(categoryID int64) {
	m := um.getMetricsByCategory(categoryID).Metrics
	m.TotalChallenges = m.TotalChallenges + 1
}

func (um *UserMetrics) setTotalOpenedClaims(categoryID, openedClaims int64) {
	m := um.getMetricsByCategory(categoryID).Metrics
	m.TotalOpenedClaims = openedClaims
}

func (um *UserMetrics) addAmoutAtStake(categoryID int64, amount sdk.Coin) {
	m := um.getMetricsByCategory(categoryID).Metrics
	m.TotalAmountAtStake = m.TotalAmountAtStake.Add(amount)
}

func (um *UserMetrics) addStakedAmount(categoryID int64, amount sdk.Coin) {
	m := um.getMetricsByCategory(categoryID).Metrics
	m.TotalAmountStaked = m.TotalAmountStaked.Add(amount)
}

func (um *UserMetrics) addBackedAmount(categoryID int64, amount sdk.Coin) {
	m := um.getMetricsByCategory(categoryID).Metrics
	m.TotalAmountBacked = m.TotalAmountBacked.Add(amount)
}

func (um *UserMetrics) addChallengedAmount(categoryID int64, amount sdk.Coin) {
	m := um.getMetricsByCategory(categoryID).Metrics
	m.TotalAmountChallenged = m.TotalAmountChallenged.Add(amount)
}

func (um *UserMetrics) addStakeLost(categoryID int64, amount sdk.Coin) {
	m := um.getMetricsByCategory(categoryID).Metrics
	m.StakeLost = m.StakeLost.Add(amount)
}

func (um *UserMetrics) addInterestEarned(categoryID int64, amount sdk.Coin) {
	m := um.getMetricsByCategory(categoryID).Metrics
	m.InterestEarned = m.InterestEarned.Add(amount)
}

func (um *UserMetrics) addStakeEarned(categoryID int64, amount sdk.Coin) {
	m := um.getMetricsByCategory(categoryID).Metrics
	m.StakeEarned = m.StakeEarned.Add(amount)
}

func (um *UserMetrics) addCredEarned(categoryID int64, amount sdk.Coin) {
	m := um.getMetricsByCategory(categoryID)
	m.CredEarned = m.CredEarned.Add(amount)
}

func (um *UserMetrics) addRunningBalance(amount sdk.Coin) {
	if amount.Denom == app.StakeDenom {
		um.RunningBalance = um.RunningBalance.Add(amount)
	}
}

// HandleMetrics dumps metrics per user basis.
func (ta *TruAPI) HandleMetrics(w http.ResponseWriter, r *http.Request) {
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
	stories := make([]story.Story, 0)

	res, err := ta.RunQuery("stories/all", struct{}{})
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	err = json.Unmarshal(res, &stories)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
		return
	}

	categories := ta.allCategoriesResolver(r.Context(), struct{}{})
	if len(categories) == 0 {
		render.Error(w, r, "no categories found", http.StatusInternalServerError)
		return
	}
	mappedCategories := make(map[int64]category.Category)

	for _, cat := range categories {
		mappedCategories[cat.ID] = cat
	}
	metricsSummary := &MetricsSummary{
		Users:      make(map[string]*UserMetrics),
		Categories: mappedCategories,
	}
	mappedStories := make(map[int64]int)
	mapUserStakeByStoryIDKey := func(user string, storyID int64) string {
		return fmt.Sprintf("%s:%d", user, storyID)
	}
	mapUserStakeByStoryID := make(map[string]sdk.Coin)
	for idx, s := range stories {
		if !s.Timestamp.CreatedTime.Before(beforeDate) {
			continue
		}

		mappedStories[s.ID] = idx
		backingAmount := sdk.NewCoin(app.StakeDenom, sdk.NewInt(0))
		challengeAmount := sdk.NewCoin(app.StakeDenom, sdk.NewInt(0))
		metricsSummary.GetUserMetrics(s.Creator.String()).increaseClaimsCount(s.CategoryID)

		// get backings and challenges
		backings := ta.backingsResolver(r.Context(), app.QueryByIDParams{ID: s.ID})
		for _, b := range backings {
			if !b.Timestamp().CreatedTime.Before(beforeDate) {
				continue
			}
			backingAmount = backingAmount.Add(b.Amount())
			creator := b.Creator().String()
			mapUserStakeByStoryID[mapUserStakeByStoryIDKey(creator, b.StoryID())] = b.Amount()
			backerMetrics := metricsSummary.GetUserMetrics(creator)
			backerMetrics.addStakedAmount(s.CategoryID, b.Amount())
			backerMetrics.addBackedAmount(s.CategoryID, b.Amount())
			backerMetrics.increaseTotalBackings(s.CategoryID)
			if s.Status == story.Pending {
				backerMetrics.addAmoutAtStake(s.CategoryID, b.Amount())
			}
			argument := ta.argumentResolver(r.Context(), app.QueryArgumentByID{ID: b.ArgumentID, Raw: true})

			if argument.ID == 0 {
				continue
			}

			if argument.Creator.String() == creator {
				backerMetrics.increaseArgumentsCount(s.CategoryID)
			}

			if argument.Creator.String() != creator {
				backerMetrics.increaseEndorsementsGivenCount(s.CategoryID)
			}

		}

		challenges := ta.challengesResolver(r.Context(), app.QueryByIDParams{ID: s.ID})
		for _, c := range challenges {
			if !c.Timestamp().CreatedTime.Before(beforeDate) {
				continue
			}
			challengeAmount = challengeAmount.Add(c.Amount())
			creator := c.Creator().String()
			mapUserStakeByStoryID[mapUserStakeByStoryIDKey(creator, c.StoryID())] = c.Amount()
			challengerMetrics := metricsSummary.GetUserMetrics(creator)
			challengerMetrics.addStakedAmount(s.CategoryID, c.Amount())
			challengerMetrics.addChallengedAmount(s.CategoryID, c.Amount())
			challengerMetrics.increaseTotalChallenges(s.CategoryID)
			if s.Status == story.Pending {
				challengerMetrics.addAmoutAtStake(s.CategoryID, c.Amount())
			}

			argument := ta.argumentResolver(r.Context(), app.QueryArgumentByID{ID: c.ArgumentID, Raw: true})

			if argument.ID == 0 {
				continue
			}

			if argument.Creator.String() == creator {
				challengerMetrics.increaseArgumentsCount(s.CategoryID)
			}

			if argument.Creator.String() != creator {
				challengerMetrics.increaseEndorsementsGivenCount(s.CategoryID)
			}
		}
		// only check expired
		if s.Status == story.Pending {
			continue
		}
		// Check if backings lost
		if backingAmount.IsLT(challengeAmount) {
			for _, b := range backings {
				metricsSummary.GetUserMetrics(b.Creator().String()).addStakeLost(s.CategoryID, b.Amount())
			}
		}

		// Check if challenges lost
		if challengeAmount.IsLT(backingAmount) {
			for _, c := range challenges {
				metricsSummary.GetUserMetrics(c.Creator().String()).addStakeLost(s.CategoryID, c.Amount())
			}
		}
	}

	type storyRewardResult struct {
		CategoryID    int64
		Reward        *sdk.Coin
		StakeReturned *sdk.Coin
	}

	getUser := func(ctx context.Context, address string) users.User {
		res := ta.usersResolver(ctx, users.QueryUsersByAddressesParams{Addresses: []string{address}})
		if len(res) > 0 {
			return res[0]
		}
		return users.User{}
	}

	getInitialStakeBalance := func(address string) int64 {
		initialStakeBalance, err := ta.DBClient.InitialStakeBalanceByAddress(address)
		if err != nil {
			return 0
		}

		return int64(initialStakeBalance.InitialBalance)
	}

	users := make([]db.TwitterProfile, 0)
	err = ta.DBClient.FindAll(&users)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
	}
	for _, userProfile := range users {
		userAddress := userProfile.Address
		userMetrics := metricsSummary.GetUserMetrics(userAddress)
		user := getUser(r.Context(), userAddress)
		userMetrics.Balance = sdk.NewCoin(app.StakeDenom, user.Coins.AmountOf(app.StakeDenom))
		userMetrics.RunningBalance = sdk.NewInt64Coin(app.StakeDenom, getInitialStakeBalance(userAddress))

		for cID, c := range metricsSummary.Categories {
			cm := userMetrics.getMetricsByCategory(cID)
			cm.CredEarned = sdk.NewCoin(c.Denom(), sdk.NewInt(0))
			cm.CategoryName = c.Title
		}
		profile, err := ta.DBClient.TwitterProfileByAddress(userAddress)
		if profile != nil && err == nil {
			userMetrics.UserName = profile.Username
		}

		txs := ta.transactionsResolver(r.Context(), app.QueryByCreatorParams{Creator: userAddress})
		userStoryResults := make(map[int64]*storyRewardResult)
		for _, tx := range txs {
			if !tx.Timestamp.CreatedTime.Before(beforeDate) {
				continue
			}

			userMetrics.addRunningBalance(tx.Amount)

			switch tx.TransactionType {
			case trubank.Interest:
				i, ok := mappedStories[tx.GroupID]
				if !ok {
					continue
				}
				s := stories[i]
				userMetrics.addInterestEarned(s.CategoryID, tx.Amount)
			case trubank.BackingLike:
				fallthrough
			case trubank.ChallengeLike:
				i, ok := mappedStories[tx.GroupID]
				if !ok {
					continue
				}
				s := stories[i]
				userMetrics.addCredEarned(s.CategoryID, tx.Amount)
				userMetrics.increaseEndorsementsReceivedCount(s.CategoryID)
			// this three transactions are related to finished expired stories.
			case trubank.RewardPool:
				fallthrough
			case trubank.BackingReturned:
				fallthrough
			case trubank.ChallengeReturned:
				i, ok := mappedStories[tx.GroupID]
				if !ok {
					continue
				}
				s := stories[i]
				if s.Status != story.Expired {
					continue
				}
				storyReward, ok := userStoryResults[tx.GroupID]
				if !ok {
					storyReward = &storyRewardResult{}
					storyReward.CategoryID = s.CategoryID
					userStoryResults[tx.GroupID] = storyReward
				}
				if tx.TransactionType == trubank.RewardPool {

					reward := sdk.NewCoin(tx.Amount.Denom, sdk.NewInt(tx.Amount.Amount.Int64()))
					storyReward.Reward = &reward
				}

				if tx.TransactionType == trubank.BackingReturned || tx.TransactionType == trubank.ChallengeReturned {
					returned := sdk.NewCoin(tx.Amount.Denom, sdk.NewInt(tx.Amount.Amount.Int64()))
					storyReward.StakeReturned = &returned
				}
			}
		}

		for storyID, storyResult := range userStoryResults {
			// majority was not reached and we performed a refund
			if storyResult.Reward == nil {
				continue
			}
			// this is the case after we introduced two transactions to reward an user
			if storyResult.StakeReturned != nil {
				userMetrics.addStakeEarned(storyResult.CategoryID, *storyResult.Reward)
				continue
			}
			// this will be the case where we will need to deduct staked amount from reward to get net value
			stakedAmount, ok := mapUserStakeByStoryID[mapUserStakeByStoryIDKey(userAddress, storyID)]
			if !ok {
				continue
			}
			reward := storyResult.Reward.Sub(stakedAmount)
			// stake was returned
			if reward.IsZero() {
				continue
			}

			// this should not happen for any reason but just adding a safe check point.
			if reward.IsNegative() {
				continue
			}
			userMetrics.addStakeEarned(storyResult.CategoryID, reward)

		}
	}
	openedClaims, err := ta.DBClient.OpenedClaimsSummary(beforeDate)
	if err != nil {
		render.Error(w, r, err.Error(), http.StatusInternalServerError)
	}
	for _, userOpenedClaims := range openedClaims {
		userMetrics := metricsSummary.GetUserMetrics(userOpenedClaims.Address)
		userMetrics.setTotalOpenedClaims(userOpenedClaims.CategoryID, userOpenedClaims.OpenedClaims)
	}
	render.JSON(w, r, metricsSummary, http.StatusOK)

}
