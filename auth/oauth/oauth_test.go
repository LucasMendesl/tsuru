// Copyright 2013 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package oauth

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/globalsign/mgo/bson"
	"github.com/tsuru/config"
	"github.com/tsuru/tsuru/auth"
	"github.com/tsuru/tsuru/db"
	authTypes "github.com/tsuru/tsuru/types/auth"
	"golang.org/x/oauth2"
	check "gopkg.in/check.v1"
)

func (s *S) TestOAuthLoginWithoutCode(c *check.C) {
	scheme := oAuthScheme{}
	params := make(map[string]string)
	params["redirectUrl"] = "http://localhost"
	_, err := scheme.Login(context.TODO(), params)
	c.Assert(err, check.Equals, ErrMissingCodeError)
}

func (s *S) TestOAuthLoginWithoutRedirectUrl(c *check.C) {
	scheme := oAuthScheme{}
	params := make(map[string]string)
	params["code"] = "abcdefg"
	_, err := scheme.Login(context.TODO(), params)
	c.Assert(err, check.Equals, ErrMissingCodeRedirectURL)
}

func (s *S) TestOAuthLogin(c *check.C) {
	scheme := oAuthScheme{}
	s.rsps["/token"] = `access_token=my_token`
	s.rsps["/user"] = `{"email":"rand@althor.com"}`
	params := make(map[string]string)
	params["code"] = "abcdefg"
	params["redirectUrl"] = "http://localhost"
	token, err := scheme.Login(context.TODO(), params)
	c.Assert(err, check.IsNil)
	c.Assert(token.GetValue(), check.Equals, "my_token")
	c.Assert(token.GetUserName(), check.Equals, "rand@althor.com")
	c.Assert(token.IsAppToken(), check.Equals, false)
	u, err := token.User()
	c.Assert(err, check.IsNil)
	c.Assert(u.Email, check.Equals, "rand@althor.com")
	c.Assert(s.reqs, check.HasLen, 2)
	c.Assert(s.reqs[0].URL.Path, check.Equals, "/token")
	c.Assert(s.bodies[0], check.Equals, "code=abcdefg&grant_type=authorization_code&redirect_uri=http%3A%2F%2Flocalhost")
	c.Assert(s.reqs[1].URL.Path, check.Equals, "/user")
	c.Assert(s.reqs[1].Header.Get("Authorization"), check.Equals, "Bearer my_token")
	dbToken, err := getToken("my_token")
	c.Assert(err, check.IsNil)
	c.Assert(dbToken.AccessToken, check.Equals, "my_token")
	c.Assert(dbToken.UserEmail, check.Equals, "rand@althor.com")
}

func (s *S) TestOAuthLoginRegistrationDisabled(c *check.C) {
	config.Set("auth:user-registration", false)
	defer config.Set("auth:user-registration", true)
	scheme := oAuthScheme{}
	s.rsps["/token"] = `access_token=my_token`
	s.rsps["/user"] = `{"email":"rand@althor.com"}`
	params := make(map[string]string)
	params["code"] = "abcdefg"
	params["redirectUrl"] = "http://localhost"
	_, err := scheme.Login(context.TODO(), params)
	c.Assert(err, check.Equals, authTypes.ErrUserNotFound)
}

func (s *S) TestOAuthLoginEmptyToken(c *check.C) {
	scheme := oAuthScheme{}
	s.rsps["/token"] = `access_token=`
	params := make(map[string]string)
	params["code"] = "abcdefg"
	params["redirectUrl"] = "http://localhost"
	_, err := scheme.Login(context.TODO(), params)
	c.Assert(err, check.ErrorMatches, `.*missing access_token.*`)
	c.Assert(s.reqs, check.HasLen, 1)
	c.Assert(s.reqs[0].URL.Path, check.Equals, "/token")
}

func (s *S) TestOAuthLoginEmptyEmail(c *check.C) {
	scheme := oAuthScheme{}
	s.rsps["/token"] = `access_token=my_token`
	s.rsps["/user"] = `{"email":""}`
	params := make(map[string]string)
	params["code"] = "abcdefg"
	params["redirectUrl"] = "http://localhost"
	_, err := scheme.Login(context.TODO(), params)
	c.Assert(err, check.Equals, ErrEmptyUserEmail)
	c.Assert(s.reqs, check.HasLen, 2)
	c.Assert(s.reqs[0].URL.Path, check.Equals, "/token")
	c.Assert(s.reqs[1].URL.Path, check.Equals, "/user")
}

func (s *S) TestOAuthName(c *check.C) {
	scheme := oAuthScheme{}
	name := scheme.Name()
	c.Assert(name, check.Equals, "oauth")
}

func (s *S) TestOAuthInfo(c *check.C) {
	scheme := oAuthScheme{}
	info, err := scheme.Info(context.TODO())
	c.Assert(err, check.IsNil)
	c.Assert(info["authorizeUrl"], check.Matches, s.server.URL+"/auth.*")
	c.Assert(info["authorizeUrl"], check.Matches, ".*client_id=clientid.*")
	c.Assert(info["authorizeUrl"], check.Matches, ".*redirect_uri=__redirect_url__.*")
	c.Assert(info["port"], check.Equals, "0")
}

func (s *S) TestOAuthInfoWithPort(c *check.C) {
	config.Set("auth:oauth:callback-port", 9009)
	defer config.Set("auth:oauth:callback-port", nil)
	scheme := oAuthScheme{}
	info, err := scheme.Info(context.TODO())
	c.Assert(err, check.IsNil)
	c.Assert(info["port"], check.Equals, "9009")
}

func (s *S) TestOAuthParse(c *check.C) {
	b := ioutil.NopCloser(bytes.NewBufferString(`{"email":"x@x.com"}`))
	rsp := &http.Response{Body: b, StatusCode: http.StatusOK}
	parser := &oAuthScheme{}
	email, err := parser.parse(rsp)
	c.Assert(err, check.IsNil)
	c.Assert(email, check.DeepEquals, userData{Email: "x@x.com"})
}

func (s *S) TestOAuthParseWithGroups(c *check.C) {
	b := ioutil.NopCloser(bytes.NewBufferString(`{"email":"x@x.com", "groups": ["g1", "g2"]}`))
	rsp := &http.Response{Body: b, StatusCode: http.StatusOK}
	parser := &oAuthScheme{}
	email, err := parser.parse(rsp)
	c.Assert(err, check.IsNil)
	c.Assert(email, check.DeepEquals, userData{Email: "x@x.com", Groups: []string{"g1", "g2"}})
}

func (s *S) TestOAuthParseInvalid(c *check.C) {
	b := ioutil.NopCloser(bytes.NewBufferString(`{xxxxxxx}`))
	rsp := &http.Response{Body: b, StatusCode: http.StatusOK}
	parser := &oAuthScheme{}
	_, err := parser.parse(rsp)
	c.Assert(err, check.ErrorMatches, `unable to parse user data: {xxxxxxx}: invalid character.*`)
}

func (s *S) TestOAuthParseInvalidStatus(c *check.C) {
	b := ioutil.NopCloser(bytes.NewBufferString(`invalid token`))
	rsp := &http.Response{Body: b, StatusCode: http.StatusUnauthorized}
	parser := &oAuthScheme{}
	_, err := parser.parse(rsp)
	c.Assert(err, check.ErrorMatches, `unexpected user data response 401: invalid token`)
}

func (s *S) TestOAuthAuth(c *check.C) {
	existing := tokenWrapper{Token: oauth2.Token{AccessToken: "myvalidtoken"}, UserEmail: "x@x.com"}
	err := existing.save()
	c.Assert(err, check.IsNil)
	scheme := oAuthScheme{}
	token, err := scheme.Auth(context.TODO(), "bearer myvalidtoken")
	c.Assert(err, check.IsNil)
	c.Assert(s.reqs, check.HasLen, 0)
	c.Assert(token.GetValue(), check.Equals, "myvalidtoken")
}

func (s *S) TestOAuthAuth_WhenTokenHasExpired(c *check.C) {
	token := tokenWrapper{
		Token: oauth2.Token{
			AccessToken: "myexpiredtoken",
			Expiry:      time.Now().Add(time.Minute * -1),
		},
		UserEmail: "x@x.com",
	}
	err := token.save()
	c.Assert(err, check.IsNil)
	scheme := oAuthScheme{}
	_, err = scheme.Auth(context.TODO(), "bearer myexpiredtoken")
	c.Assert(s.reqs, check.HasLen, 0)
	c.Assert(err, check.Equals, auth.ErrInvalidToken)
}

func (s *S) TestOAuthAppLogin(c *check.C) {
	scheme := oAuthScheme{}
	token, err := scheme.AppLogin(context.TODO(), "myApp")
	c.Assert(err, check.IsNil)
	c.Assert(token.IsAppToken(), check.Equals, true)
	c.Assert(token.GetAppName(), check.Equals, "myApp")
}

func (s *S) TestOAuthAuthWithAppToken(c *check.C) {
	scheme := oAuthScheme{}
	appToken, err := scheme.AppLogin(context.TODO(), "myApp")
	c.Assert(err, check.IsNil)
	token, err := scheme.Auth(context.TODO(), "bearer "+appToken.GetValue())
	c.Assert(err, check.IsNil)
	c.Assert(s.reqs, check.HasLen, 0)
	c.Assert(token.IsAppToken(), check.Equals, true)
	c.Assert(token.GetAppName(), check.Equals, "myApp")
	c.Assert(token.GetValue(), check.Equals, appToken.GetValue())
}

func (s *S) TestOAuthCreate(c *check.C) {
	scheme := oAuthScheme{}
	user := auth.User{Email: "x@x.com", Password: "something"}
	_, err := scheme.Create(context.TODO(), &user)
	c.Assert(err, check.IsNil)
	dbUser, err := auth.GetUserByEmail(user.Email)
	c.Assert(err, check.IsNil)
	c.Assert(dbUser.Email, check.Equals, user.Email)
	c.Assert(dbUser.Password, check.Equals, "")
}

func (s *S) TestOAuthRemove(c *check.C) {
	scheme := oAuthScheme{}
	s.rsps["/token"] = `access_token=my_token`
	s.rsps["/user"] = `{"email":"rand@althor.com"}`
	params := make(map[string]string)
	params["code"] = "abcdefg"
	params["redirectUrl"] = "http://localhost"
	token, err := scheme.Login(context.TODO(), params)
	c.Assert(err, check.IsNil)
	u, err := auth.ConvertNewUser(token.User())
	c.Assert(err, check.IsNil)
	err = scheme.Remove(context.TODO(), u)
	c.Assert(err, check.IsNil)
	conn, err := db.Conn()
	c.Assert(err, check.IsNil)
	defer conn.Close()
	var tokens []tokenWrapper
	coll := collection()
	defer coll.Close()
	err = coll.Find(bson.M{"useremail": "rand@althor.com"}).All(&tokens)
	c.Assert(err, check.IsNil)
	c.Assert(tokens, check.HasLen, 0)
	_, err = auth.GetUserByEmail("rand@althor.com")
	c.Assert(err, check.Equals, authTypes.ErrUserNotFound)
}
