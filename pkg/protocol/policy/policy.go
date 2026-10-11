// Package policy is the Flash cross-domain handshake answered on :843
// and in-band on the game socket.
package policy

// Response matches GameClient in the DarkOrbit 10.0 emulator, including the NUL terminator.
const Response = "<?xml version=\"1.0\"?>\r\n" +
	"<!DOCTYPE cross-domain-policy SYSTEM \"/xml/dtds/cross-domain-policy.dtd\">\r\n" +
	"<cross-domain-policy>\r\n" +
	"<allow-access-from domain=\"*\" to-ports=\"*\" />\r\n" +
	"</cross-domain-policy>\x00"

// Request is the token the Flash player sends before any game packet.
const Request = "<policy-file-request/>"
