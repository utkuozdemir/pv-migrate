package ssh

type ed25519message struct {
	CipherName   string
	KdfName      string
	KdfOpts      string
	NumKeys      uint32
	PubKey       []byte
	PrivKeyBlock []byte
}

type ed25519pk1 struct {
	Check1  uint32
	Check2  uint32
	Keytype string
	Pub     []byte
	Priv    []byte
	Comment string
	Pad     []byte `ssh:"rest"`
}
