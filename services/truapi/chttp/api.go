package chttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	truCtx "github.com/TruStory/octopus/services/truapi/context"
	"github.com/TruStory/truchain/x/users"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/utils"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authtxb "github.com/cosmos/cosmos-sdk/x/auth/client/txbuilder"
	"github.com/gorilla/mux"
	"github.com/oklog/ulid"
	amino "github.com/tendermint/go-amino"
	abci "github.com/tendermint/tendermint/abci/types"
	tcmn "github.com/tendermint/tendermint/libs/common"
	trpctypes "github.com/tendermint/tendermint/rpc/core/types"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/sync/errgroup"
)

// MsgTypes is a map of `Msg` type names to empty instances
type MsgTypes map[string]interface{}

// App is implemented by a Cosmos app client to provide chain functionality to the API
type App interface {
	RegisterKey(tcmn.HexBytes, string) (sdk.AccAddress, uint64, sdk.Coins, error)
	RunQuery(string, interface{}) abci.ResponseQuery
	DeliverPresigned(auth.StdTx) (*trpctypes.ResultBroadcastTxCommit, error)
}

// API presents the functionality of a Cosmos app over HTTP
type API struct {
	apiCtx    truCtx.TruAPIContext
	Supported MsgTypes
	router    *mux.Router
}

// NewAPI creates an `API` struct from a client context and a `MsgTypes` schema
func NewAPI(apiCtx truCtx.TruAPIContext, supported MsgTypes) *API {
	a := API{apiCtx: apiCtx, Supported: supported, router: mux.NewRouter()}
	return &a
}

// HandleFunc registers a `chttp.Handler` on the API router
func (a *API) HandleFunc(path string, h Handler) {
	a.router.HandleFunc(path, h.HandlerFunc())
}

// Subrouter returns a mux subrouter.
func (a *API) Subrouter(path string) *mux.Router {
	return a.router.PathPrefix(path).Subrouter()
}

// PathPrefix adds a http.Handler to a path prefix
func (a *API) PathPrefix(path string, handler http.Handler) {
	a.router.PathPrefix(path).HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	})
}

// Handle registers a http.Handler
func (a *API) Handle(path string, handler http.Handler) {
	a.router.Handle(path, handler)
}

// Use registers a middleware on the API router
func (a *API) Use(mw func(http.Handler) http.Handler) {
	a.router.Use(mw)
}

// ListenAndServe serves HTTP using the API router
func (a *API) ListenAndServe(addr string) error {
	letsEncryptEnabled := a.apiCtx.Config.Host.HTTPSEnabled == true
	if !letsEncryptEnabled {
		return http.ListenAndServe(addr, a.router)
	}
	return a.listenAndServeTLS()
}

func (a *API) listenAndServeTLS() error {
	m := &autocert.Manager{
		Cache:      autocert.DirCache(a.apiCtx.Config.Host.HTTPSCacheDir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(a.apiCtx.Config.Host.Name),
	}
	httpServer := &http.Server{
		Addr:    ":http",
		Handler: http.HandlerFunc(redirectHandler),
	}
	secureServer := &http.Server{
		Addr:      ":https",
		Handler:   a.router,
		TLSConfig: m.TLSConfig(),
	}

	g, ctx := errgroup.WithContext(context.Background())
	g.Go(func() error {
		return httpServer.ListenAndServe()
	})
	g.Go(func() error {
		return secureServer.ListenAndServeTLS("", "")
	})
	g.Go(func() error {
		select {
		case <-ctx.Done():
			_ = httpServer.Shutdown(ctx)
			_ = secureServer.Shutdown(ctx)
			return nil
		}
	})

	return g.Wait()
}

func redirectHandler(w http.ResponseWriter, r *http.Request) {
	url := fmt.Sprintf("https://%s%s", r.Host, r.URL.Path)
	http.Redirect(w, r, url, http.StatusMovedPermanently)
}

// RegisterKey generates a new address/account for a public key
func (a *API) RegisterKey(k tcmn.HexBytes, algo string) (
	accAddr sdk.AccAddress, accNum uint64, coins sdk.Coins, err error) {

	var addr []byte
	if string(algo[0]) == "*" {
		addr = []byte("cosmostestingaddress")
		algo = algo[1:]
	} else {
		addr = generateAddress()
	}

	_, err = a.signAndBroadcastRegistrationTx(addr, k, algo)
	if err != nil {
		return
	}

	addresses := users.QueryUsersByAddressesParams{
		Addresses: []string{sdk.AccAddress(addr).String()},
	}
	result, err := a.RunQuery(users.QueryPath, addresses)
	if err != nil {
		return
	}

	var u []users.User
	err = amino.UnmarshalJSON(result, &u)
	if err != nil {
		panic(err)
	}
	if len(u) == 0 {
		err = errors.New("Unable to locate account " + string(addr))
		return
	}
	stored := u[0]

	return sdk.AccAddress(addr), stored.AccountNumber, stored.Coins, nil
}

// GenerateAddress returns the first 20 characters of a ULID (https://github.com/oklog/ulid)
func generateAddress() []byte {
	t := time.Now()
	entropy := ulid.Monotonic(rand.New(rand.NewSource(t.UnixNano())), 0)
	ulidaddr := ulid.MustNew(ulid.Timestamp(t), entropy)
	addr := []byte(ulidaddr.String())[:20]

	return addr
}

// Steps:
// get --home flag right.. /Users/blockshane/.truapid
// created an account with trucli keys add
// added this to truchaind with `truchaind add-genesis-account`
// then start chain

func (a *API) signAndBroadcastRegistrationTx(addr []byte, k tcmn.HexBytes, algo string) (sdk.TxResponse, error) {
	cliCtx := a.apiCtx
	config := cliCtx.Config.Registrar

	registrarAddr, err := sdk.AccAddressFromBech32(config.Addr)
	if err := cliCtx.EnsureAccountExistsFromAddr(registrarAddr); err != nil {
		panic(err)
	}

	msg := users.RegisterKeyMsg{
		Address:    addr,
		PubKey:     k,
		PubKeyAlgo: algo,
		Coins:      nil,
	}
	err = msg.ValidateBasic()
	if err != nil {
		panic(err)
	}

	// build and sign the transaction
	seq, err := cliCtx.GetAccountSequence(registrarAddr)
	if err != nil {
		panic(err)
	}
	txBldr := authtxb.NewTxBuilderFromCLI().WithSequence(seq).WithTxEncoder(utils.GetTxEncoder(cliCtx.Codec))
	txBytes, err := txBldr.BuildAndSign(config.Name, config.Pass, []sdk.Msg{msg})
	if err != nil {
		panic(err)
	}

	// broadcast to a Tendermint node
	res, err := cliCtx.BroadcastTx(txBytes)
	cliCtx.PrintOutput(res)

	return res, nil
}

// RunQuery dispatches a query (path + params) to the Tendermint node
func (a *API) RunQuery(path string, params interface{}) ([]byte, error) {
	paramBytes, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	res, err := a.apiCtx.QueryWithData("/custom/"+path, paramBytes)
	if err != nil {
		return res, err
	}

	return res, nil
}

// DeliverPresigned dispatches a pre-signed transaction to the Tendermint node
func (a *API) DeliverPresigned(tx auth.StdTx) (sdk.TxResponse, error) {
	txBytes := a.apiCtx.Codec.MustMarshalBinaryLengthPrefixed(tx)
	return a.apiCtx.WithBroadcastMode(client.BroadcastBlock).BroadcastTx(txBytes)
}