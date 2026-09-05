// chartdoctor exercises the deployed server's supported password/TOTP ceremony
// and persists its real CLI session in private fixture custody. It never seeds
// authentication rows or substitutes health responses.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
)

func main() {
	origin := flag.String("origin", "", "owned chart loopback port-forward origin")
	privateDir := flag.String("private-dir", "", "private fixture custody directory")
	binary := flag.String("binary", "", "same-source native CLI binary")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := run(ctx, *origin, *privateDir, *binary); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, origin, privateDir, binary string) error {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("chartdoctor requires the owned loopback port-forward origin")
	}
	info, err := os.Lstat(privateDir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("chartdoctor requires a private 0700 fixture directory")
	}
	authority, err := os.ReadFile(filepath.Join(privateDir, "authority"))
	if err != nil || len(bytes.TrimSpace(authority)) == 0 {
		return errors.New("chartdoctor could not read the privately delivered establishment authority")
	}
	passwordBytes := make([]byte, 32)
	if _, err := rand.Read(passwordBytes); err != nil {
		return errors.New("chartdoctor password generation failed")
	}
	password := base64.RawURLEncoding.EncodeToString(passwordBytes)
	if err := privateWrite(filepath.Join(privateDir, "password"), []byte(password)); err != nil {
		return err
	}
	trust := cli.TrustEntry{Name: "chart", Origin: origin}
	client, err := cli.NewClient(trust, "")
	if err != nil {
		return errors.New("chartdoctor could not construct the loopback client")
	}
	if err := client.Do(ctx, http.MethodPost, api.PathPrefix+"/auth/credential/establish", apigen.EstablishCredentialRequest{
		Authority: strings.TrimSpace(string(authority)), Password: password,
	}, nil); err != nil {
		return errors.New("chartdoctor credential establishment failed")
	}
	var login apigen.LoginResult
	if err := client.Do(ctx, http.MethodPost, api.PathPrefix+"/auth/local/login", apigen.LocalLoginRequest{
		Username: "chart-doctor", Password: password,
	}, &login); err != nil {
		return errors.New("chartdoctor password login failed")
	}
	client, err = sessionClient(trust, login)
	if err != nil {
		return err
	}
	var enrollment apigen.TotpEnrolStartResult
	if err := client.Do(ctx, http.MethodPost, api.PathPrefix+"/auth/totp/enrol/start", apigen.TotpEnrolStartRequest{Password: password}, &enrollment); err != nil {
		return errors.New("chartdoctor TOTP enrollment failed")
	}
	if err := privateWrite(filepath.Join(privateDir, "totp-uri"), []byte(enrollment.OtpauthUri)); err != nil {
		return err
	}
	uri, err := url.Parse(enrollment.OtpauthUri)
	if err != nil || uri.Query().Get("secret") == "" {
		return errors.New("chartdoctor received an invalid TOTP provisioning URI")
	}
	secret := uri.Query().Get("secret")
	confirmationTime := time.Now()
	confirmedStep := confirmationTime.Unix() / 30
	code, err := totp.GenerateCode(secret, confirmationTime)
	if err != nil {
		return errors.New("chartdoctor could not generate the confirmation code")
	}
	login = apigen.LoginResult{}
	if err := client.Do(ctx, http.MethodPost, api.PathPrefix+"/auth/totp/enrol/confirm", apigen.TotpCodeRequest{Code: code}, &login); err != nil {
		return errors.New("chartdoctor TOTP confirmation failed")
	}
	client, err = sessionClient(trust, login)
	if err != nil {
		return err
	}
	// Confirmation consumed this time step. Wait for the real authenticator's
	// next step, rather than changing the server clock or bypassing replay checks.
	delay := time.Until(time.Unix((confirmedStep+1)*30, 0).Add(time.Second))
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	code, err = totp.GenerateCode(secret, time.Now())
	if err != nil {
		return errors.New("chartdoctor could not generate the step-up code")
	}
	login = apigen.LoginResult{}
	if err := client.Do(ctx, http.MethodPost, api.PathPrefix+"/auth/totp/step-up", apigen.TotpCodeRequest{Code: code}, &login); err != nil {
		return errors.New("chartdoctor TOTP step-up failed")
	}
	if _, err := sessionClient(trust, login); err != nil || !slices.Contains(login.Session.Assurance.Factors, "totp") {
		return errors.New("chartdoctor did not receive a factor-assured CLI session")
	}
	stateDir := filepath.Join(privateDir, "cli-state")
	state, err := cli.NewState(cli.Env{Getenv: func(key string) string {
		if key == "HIKYO_STATE_DIR" {
			return stateDir
		}
		return ""
	}})
	if err != nil {
		return errors.New("chartdoctor could not initialize private CLI custody")
	}
	if err := state.Trust().Put(trust); err != nil {
		return errors.New("chartdoctor could not persist owned loopback trust")
	}
	if err := state.PutSession(cli.SessionArtifact{Instance: "chart", Origin: origin, Token: *login.SessionToken,
		SessionID: login.Session.Id, Principal: login.Principal.Id, ExpiresAt: login.Session.AbsoluteExpiresAt.Format(time.RFC3339Nano)}); err != nil {
		return errors.New("chartdoctor could not persist the real human session")
	}
	command := exec.CommandContext(ctx, binary, "doctor", "--instance", "chart", "--auth=human", "-o", "json")
	command.Env = []string{"HIKYO_STATE_DIR=" + stateDir}
	// Response diagnostics are metadata only. Keep even failed command output
	// private: CI prints only the validated code/severity checklist afterward.
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, runErr := command.Output()
	if err := privateWrite(filepath.Join(privateDir, "doctor.json"), output); err != nil {
		return err
	}
	if err := privateWrite(filepath.Join(privateDir, "doctor.stderr"), stderr.Bytes()); err != nil {
		return err
	}
	if runErr != nil {
		return errors.New("chartdoctor native CLI failed; inspect private fixture doctor output")
	}
	fmt.Println("chartdoctor: real password/TOTP human session authenticated native CLI doctor")
	return nil
}

func sessionClient(trust cli.TrustEntry, login apigen.LoginResult) (*cli.Client, error) {
	if login.SessionToken == nil || *login.SessionToken == "" || login.Session.Artifact != "cli" || login.Session.Id == "" || login.Principal.Id == "" {
		return nil, errors.New("chartdoctor did not receive a real CLI session")
	}
	return cli.NewClient(trust, *login.SessionToken)
}

func privateWrite(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.New("chartdoctor could not exclusively create private fixture material")
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("chartdoctor could not persist private fixture material")
	}
	return nil
}
