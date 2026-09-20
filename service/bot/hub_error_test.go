package bot

import (
	"context"
	"io"
	"net"
	"testing"
)

func TestClassifyBotAPIError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{fmtErr("empty token"), "尚未保存 Token。"},
		{fmtErr("token 为空"), "尚未保存 Token。"},
		{fmtErr("error call getMe, unauthorized, Unauthorized"), "Token 无效。"},
		{context.DeadlineExceeded, unreachableBotAPIMessage},
		{&net.DNSError{Err: "i/o timeout", Name: "api.telegram.org", IsTimeout: true}, unreachableBotAPIMessage},
		{io.EOF, unreachableBotAPIMessage},
		{fmtErr("error do request, connection refused"), unreachableBotAPIMessage},
		{fmtErr("some other telegram failure"), "some other telegram failure"},
	}
	for _, tc := range cases {
		got := classifyBotAPIError(tc.err)
		if got.Error() != tc.want {
			t.Fatalf("%v: got %q want %q", tc.err, got.Error(), tc.want)
		}
	}
}

type fmtErr string

func (e fmtErr) Error() string { return string(e) }
