package realtime

import (
	"encoding/json"
	"io"
	"log"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
	"github.com/brynnb/new-yokosuka-server/internal/store"
	"github.com/brynnb/new-yokosuka-server/internal/worldstate"
)

const timedTransitionID = "d000-door-30-to-dcha-entry-0"

func newTransitionTestClient(
	t *testing.T,
	gameSecond int,
	initialWorldID string,
) (*Hub, *Client) {
	t.Helper()
	epoch := time.UnixMilli(1_700_000_000_000)
	clock, err := worldstate.NewClock(epoch, "summer")
	if err != nil {
		t.Fatal(err)
	}
	clock.SetNowForTest(func() time.Time { return epoch.Add(time.Hour) })
	if _, err := clock.SetGameSecond(gameSecond); err != nil {
		t.Fatal(err)
	}
	hub := NewHub(
		10,
		worldstate.NewManager(clock),
		log.New(io.Discard, "", 0),
		nil,
	)
	client, err := hub.Register(nil, ConnectionMetadata{
		AccountID:   1,
		AccountType: "account",
		Character: &store.Character{
			ID: 1, AccountID: 1, Name: "Ryo",
			AvatarKey: "ryo", WorldID: initialWorldID, CurrentHP: 100,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	<-client.send // welcome
	t.Cleanup(func() { hub.Unregister(client) })
	return hub, client
}

func transitionPresence(worldID string, sequence uint64, x, z float64) protocol.Presence {
	return protocol.Presence{
		Header:      protocol.NewHeader(protocol.TypePresence),
		WorldID:     worldID,
		CharacterID: "ryo",
		AvatarID:    "ryo",
		X:           x,
		Y:           -1.8,
		Z:           z,
		Yaw:         0,
		Movement:    "idle",
		Sequence:    sequence,
		VehicleQW:   1,
	}
}

func placeAtTimedDoor(t *testing.T, hub *Hub, client *Client) {
	t.Helper()
	if !hub.HandlePresence(client, transitionPresence(
		"dobuita",
		1,
		126.362854,
		65.0345,
	)) {
		t.Fatal("source presence was rejected")
	}
	<-client.send // source-room snapshot
}

func requestTimedTransition(
	t *testing.T,
	hub *Hub,
	client *Client,
	request protocol.TransitionRequest,
) protocol.TransitionResult {
	t.Helper()
	if !hub.HandleTransitionRequest(client, request) {
		t.Fatal("transition request could not be answered")
	}
	var result protocol.TransitionResult
	if err := json.Unmarshal(<-client.send, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func validTimedRequest(requestID string) protocol.TransitionRequest {
	return protocol.TransitionRequest{
		Header:             protocol.NewHeader(protocol.TypeTransitionRequest),
		RequestID:          requestID,
		TransitionID:       timedTransitionID,
		DoorSelector:       30,
		SourceWorldID:      "dobuita",
		DestinationWorldID: "dcha",
	}
}

func TestTimedTransitionUsesAuthoritativeClosingBoundary(t *testing.T) {
	tests := []struct {
		name       string
		gameSecond int
		authorized bool
		reason     string
	}{
		{"immediately before closing", 19*60*60 + 29*60 + 59, true, transitionReasonAuthorized},
		{"at closing", 19*60*60 + 30*60, false, transitionReasonClosed},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hub, client := newTransitionTestClient(t, test.gameSecond, "dobuita")
			placeAtTimedDoor(t, hub, client)
			result := requestTimedTransition(
				t,
				hub,
				client,
				validTimedRequest("boundary-"+string(rune('a'+index))),
			)
			if result.Authorized != test.authorized || result.Reason != test.reason {
				t.Fatalf("unexpected result: %#v", result)
			}
		})
	}
}

func TestTimedTransitionRejectsMismatchedRouteAndExcessiveDistance(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*protocol.TransitionRequest)
		reason string
	}{
		{"transition", func(r *protocol.TransitionRequest) { r.TransitionID = "wrong" }, transitionReasonUnknown},
		{"source", func(r *protocol.TransitionRequest) { r.SourceWorldID = "yamanose" }, transitionReasonSourceMismatch},
		{"door", func(r *protocol.TransitionRequest) { r.DoorSelector = 31 }, transitionReasonDoorMismatch},
		{"destination", func(r *protocol.TransitionRequest) { r.DestinationWorldID = "dbhb" }, transitionReasonDestination},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hub, client := newTransitionTestClient(t, 12*60*60, "dobuita")
			placeAtTimedDoor(t, hub, client)
			request := validTimedRequest("mismatch-" + test.name)
			test.mutate(&request)
			result := requestTimedTransition(t, hub, client, request)
			if result.Authorized || result.Reason != test.reason {
				t.Fatalf("unexpected result: %#v", result)
			}
		})
	}

	hub, client := newTransitionTestClient(t, 12*60*60, "dobuita")
	if !hub.HandlePresence(client, transitionPresence("dobuita", 1, 0, 0)) {
		t.Fatal("distant source presence was rejected")
	}
	<-client.send
	result := requestTimedTransition(t, hub, client, validTimedRequest("too-far"))
	if result.Authorized || result.Reason != transitionReasonTooFar {
		t.Fatalf("unexpected distance result: %#v", result)
	}

	wrongHub, wrongClient := newTransitionTestClient(t, 12*60*60, "yamanose")
	if !wrongHub.HandlePresence(
		wrongClient,
		transitionPresence("yamanose", 1, 126.362854, 65.0345),
	) {
		t.Fatal("wrong-source presence was rejected before the access check")
	}
	<-wrongClient.send
	wrongSource := requestTimedTransition(
		t,
		wrongHub,
		wrongClient,
		validTimedRequest("wrong-current-world"),
	)
	if wrongSource.Authorized || wrongSource.Reason != transitionReasonSourceMismatch {
		t.Fatalf("unexpected current-world result: %#v", wrongSource)
	}
}

func TestClosedInteriorCannotBeEnteredByDirectPresence(t *testing.T) {
	hub, client := newTransitionTestClient(t, 19*60*60+30*60, "dobuita")
	placeAtTimedDoor(t, hub, client)
	direct := transitionPresence("dcha", 2, 0.11, -2.96)
	if hub.HandlePresence(client, direct) {
		t.Fatal("direct room change bypassed the controlled entrance")
	}
	if got := hub.presences[client.id].state.WorldID; got != "dobuita" {
		t.Fatalf("server presence moved to %q", got)
	}

	hub.LeaveWorld(client)
	if _, exists := hub.presences[client.id]; exists {
		t.Fatal("leave_world did not clear source presence")
	}
	if hub.HandlePresence(client, direct) {
		t.Fatal("leave_world followed by a direct destination bypass succeeded")
	}
	if _, exists := hub.presences[client.id]; exists {
		t.Fatal("rejected destination became authoritative")
	}
}

func TestSidebarInteriorAllowsDirectTravelAtAnyPublicTime(t *testing.T) {
	hub, client := newTransitionTestClient(t, 23*60*60, "dobuita")
	placeAtTimedDoor(t, hub, client)
	if !hub.HandlePresence(client, transitionPresence("djaz", 2, 0, 0)) {
		t.Fatal("the explicitly always-accessible sidebar interior was rejected")
	}
	<-client.send
	if got := hub.presences[client.id].state.WorldID; got != "djaz" {
		t.Fatalf("server presence moved to %q", got)
	}
}

func TestAuthorizedTransitionCommitsOneConnectionScopedArrival(t *testing.T) {
	hub, client := newTransitionTestClient(t, 19*60*60+29*60+59, "dobuita")
	placeAtTimedDoor(t, hub, client)
	authorization := requestTimedTransition(
		t,
		hub,
		client,
		validTimedRequest("authorized-arrival"),
	)
	if !authorization.Authorized || authorization.AuthorizationID == "" {
		t.Fatalf("authorization failed: %#v", authorization)
	}

	commit := protocol.TransitionCommit{
		Header:          protocol.NewHeader(protocol.TypeTransitionCommit),
		RequestID:       authorization.RequestID,
		AuthorizationID: authorization.AuthorizationID,
	}
	if !hub.HandleTransitionCommit(client, commit) {
		t.Fatal("commit could not be answered")
	}
	var committed protocol.TransitionCommitResult
	if err := json.Unmarshal(<-client.send, &committed); err != nil {
		t.Fatal(err)
	}
	if !committed.Committed || committed.Reason != transitionReasonCommitted {
		t.Fatalf("unexpected commit: %#v", committed)
	}
	if _, exists := hub.presences[client.id]; exists {
		t.Fatal("committed travel retained the source presence")
	}
	if hub.HandlePresence(client, transitionPresence(
		"dobuita",
		2,
		126.362854,
		65.0345,
	)) {
		t.Fatal("an in-flight source presence was accepted after commit")
	}
	if _, exists := hub.presences[client.id]; exists {
		t.Fatal("an in-flight source presence restored source membership")
	}
	if _, exists := hub.authorizedArrivals[client.id]; !exists {
		t.Fatal("an in-flight source presence consumed the reserved arrival")
	}
	if !hub.HandlePresence(client, transitionPresence("dcha", 3, 0.11, -2.96)) {
		t.Fatal("authorized destination presence was rejected")
	}
	<-client.send // destination snapshot
	if got := hub.presences[client.id].state.WorldID; got != "dcha" {
		t.Fatalf("arrived in %q", got)
	}
	if _, exists := hub.authorizedArrivals[client.id]; exists {
		t.Fatal("arrival authorization was not consumed")
	}

	if !hub.HandleTransitionCommit(client, commit) {
		t.Fatal("reused commit could not be answered")
	}
	var reused protocol.TransitionCommitResult
	if err := json.Unmarshal(<-client.send, &reused); err != nil {
		t.Fatal(err)
	}
	if reused.Committed || reused.Reason != transitionReasonInvalidGrant {
		t.Fatalf("authorization was reusable: %#v", reused)
	}
}

func TestTransitionCommitRechecksProximityAndConsumesRejectedGrant(t *testing.T) {
	hub, client := newTransitionTestClient(t, 12*60*60, "dobuita")
	placeAtTimedDoor(t, hub, client)
	authorization := requestTimedTransition(
		t,
		hub,
		client,
		validTimedRequest("moved-before-commit"),
	)
	if !authorization.Authorized {
		t.Fatalf("authorization failed: %#v", authorization)
	}
	if !hub.HandlePresence(client, transitionPresence("dobuita", 2, 0, 0)) {
		t.Fatal("updated source presence was rejected")
	}
	commit := protocol.TransitionCommit{
		Header:          protocol.NewHeader(protocol.TypeTransitionCommit),
		RequestID:       authorization.RequestID,
		AuthorizationID: authorization.AuthorizationID,
	}
	if !hub.HandleTransitionCommit(client, commit) {
		t.Fatal("commit could not be answered")
	}
	var denied protocol.TransitionCommitResult
	if err := json.Unmarshal(<-client.send, &denied); err != nil {
		t.Fatal(err)
	}
	if denied.Committed || denied.Reason != transitionReasonTooFar {
		t.Fatalf("unexpected commit result: %#v", denied)
	}
	if _, exists := hub.transitionGrants[client.id]; exists {
		t.Fatal("a rejected commit retained its grant")
	}
	if got := hub.presences[client.id].state.WorldID; got != "dobuita" {
		t.Fatalf("rejected commit moved presence to %q", got)
	}
	if !hub.HandleTransitionCommit(client, commit) {
		t.Fatal("reused rejected commit could not be answered")
	}
	var reused protocol.TransitionCommitResult
	if err := json.Unmarshal(<-client.send, &reused); err != nil {
		t.Fatal(err)
	}
	if reused.Committed || reused.Reason != transitionReasonInvalidGrant {
		t.Fatalf("rejected authorization was reusable: %#v", reused)
	}
}

func TestExpiredTransitionAuthorizationCannotCommit(t *testing.T) {
	hub, client := newTransitionTestClient(t, 12*60*60, "dobuita")
	placeAtTimedDoor(t, hub, client)
	authorization := requestTimedTransition(
		t,
		hub,
		client,
		validTimedRequest("expired-authorization"),
	)
	grant := hub.transitionGrants[client.id]
	grant.expiresAt = time.Now().Add(-time.Nanosecond)
	hub.transitionGrants[client.id] = grant
	if !hub.HandleTransitionCommit(client, protocol.TransitionCommit{
		Header:          protocol.NewHeader(protocol.TypeTransitionCommit),
		RequestID:       authorization.RequestID,
		AuthorizationID: authorization.AuthorizationID,
	}) {
		t.Fatal("expired commit could not be answered")
	}
	var result protocol.TransitionCommitResult
	if err := json.Unmarshal(<-client.send, &result); err != nil {
		t.Fatal(err)
	}
	if result.Committed || result.Reason != transitionReasonExpired {
		t.Fatalf("expired authorization committed: %#v", result)
	}
	if _, exists := hub.transitionGrants[client.id]; exists {
		t.Fatal("expired authorization was retained")
	}
	if _, exists := hub.authorizedArrivals[client.id]; exists {
		t.Fatal("expired authorization created an arrival")
	}
}

func TestTransitionAuthorizationDoesNotSurviveReconnect(t *testing.T) {
	hub, client := newTransitionTestClient(t, 12*60*60, "dobuita")
	placeAtTimedDoor(t, hub, client)
	authorization := requestTimedTransition(
		t,
		hub,
		client,
		validTimedRequest("old-connection"),
	)
	hub.Unregister(client)

	reconnected, err := hub.Register(nil, ConnectionMetadata{
		AccountID:   2,
		AccountType: "account",
		Character: &store.Character{
			ID: 2, AccountID: 2, Name: "Ryo Two",
			AvatarKey: "ryo", WorldID: "dobuita", CurrentHP: 100,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Unregister(reconnected)
	<-reconnected.send
	placeAtTimedDoor(t, hub, reconnected)
	if !hub.HandleTransitionCommit(reconnected, protocol.TransitionCommit{
		Header:          protocol.NewHeader(protocol.TypeTransitionCommit),
		RequestID:       authorization.RequestID,
		AuthorizationID: authorization.AuthorizationID,
	}) {
		t.Fatal("stale commit could not be answered")
	}
	var result protocol.TransitionCommitResult
	if err := json.Unmarshal(<-reconnected.send, &result); err != nil {
		t.Fatal(err)
	}
	if result.Committed || result.Reason != transitionReasonInvalidGrant {
		t.Fatalf("reconnect reused an authorization: %#v", result)
	}
}

func TestInteriorExitAndUnrestrictedTravelRemainAvailable(t *testing.T) {
	hub, client := newTransitionTestClient(t, 19*60*60+30*60, "dcha")
	if !hub.HandlePresence(client, transitionPresence("dcha", 1, 0.11, -2.96)) {
		t.Fatal("persisted interior reconnect was rejected")
	}
	<-client.send
	hub.LeaveWorld(client)
	if !hub.HandlePresence(client, transitionPresence("dobuita", 2, 126, 65)) {
		t.Fatal("interior exit was blocked after closing")
	}
	<-client.send

	if !hub.HandlePresence(client, transitionPresence("yamanose", 3, 1, 1)) {
		t.Fatal("an unrestricted world transition was rejected")
	}
	<-client.send
	if got := hub.presences[client.id].state.WorldID; got != "yamanose" {
		t.Fatalf("unrestricted transition ended in %q", got)
	}
}
