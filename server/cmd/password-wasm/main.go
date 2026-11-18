//go:build js && wasm

// This is the browser-only OPAQUE client. No password is passed to a network API.
package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"syscall/js"

	"github.com/bytemare/opaque"
	"github.com/nwverse/persona/server/internal/passwordpake"
)

func main() {
	var client *opaque.Client
	var mode string
	fn := js.FuncOf(func(_ js.Value, args []js.Value) (result any) {
		defer func() {
			if recover() != nil {
				client = nil
				// i18n-ignore: legacy browser-WASM JSON error; web/public/pake/worker.js parses this protocol, not a terminal.
				result = `{"error":"密码验证失败，请重试"}`
			}
		}()
		var in struct{ Op, Password, Message, Identity string }
		if len(args) != 1 || json.Unmarshal([]byte(args[0].String()), &in) != nil {
			// i18n-ignore: legacy browser-WASM JSON error; web/public/pake/worker.js parses this protocol, not a terminal.
			return `{"error":"无效的验证请求"}`
		}
		message, err := base64.StdEncoding.DecodeString(in.Message)
		var out []byte
		if err == nil {
			switch in.Op {
			case "register-start", "login-start":
				client, err = passwordpake.Configuration().Client()
				mode = in.Op
				if err == nil {
					password := []byte(in.Password)
					defer clear(password)
					if mode == "register-start" {
						m, e := client.RegistrationInit(password)
						err = e
						if e == nil {
							out = m.Serialize()
						}
					} else {
						m, e := client.GenerateKE1(password)
						err = e
						if e == nil {
							out = m.Serialize()
						}
					}
				}
			case "register-finish":
				if client == nil || mode != "register-start" {
					err = errors.New("state")
					break
				}
				m, e := client.Deserialize.RegistrationResponse(message)
				err = e
				if e == nil {
					r, _, e := client.RegistrationFinalize(m, passwordpake.ClientIdentity, []byte(in.Identity))
					err = e
					if e == nil {
						out = r.Serialize()
					}
				}
				client = nil
			case "login-finish":
				if client == nil || mode != "login-start" {
					err = errors.New("state")
					break
				}
				m, e := client.Deserialize.KE2(message)
				err = e
				if e == nil {
					r, _, _, e := client.GenerateKE3(m, passwordpake.ClientIdentity, []byte(in.Identity))
					err = e
					if e == nil {
						out = r.Serialize()
					}
				}
				client = nil
			default:
				err = errors.New("operation")
			}
		}
		if err != nil {
			client = nil
			// i18n-ignore: legacy browser-WASM JSON error; web/public/pake/worker.js parses this protocol, not a terminal.
			return `{"error":"密码不正确或验证未完成，请重试"}`
		}
		b, _ := json.Marshal(map[string]string{"message": base64.StdEncoding.EncodeToString(out)})
		return string(b)
	})
	js.Global().Set("personaPake", fn)
	select {}
}
