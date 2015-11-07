// jwe implements JWE https://tools.ietf.org/html/rfc7516

package jwe

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/lestrrat/go-jwx/buffer"
	"github.com/lestrrat/go-jwx/internal/emap"
	"github.com/lestrrat/go-jwx/jwa"
)

func NewHeader() *Header {
	return &Header{
		EssentialHeader: &EssentialHeader{},
		PrivateParams:   map[string]interface{}{},
	}
}

func NewMessage() *Message {
	return &Message{
		AEADParts: AEADParts{},
	}
}

func (h Header) MarshalJSON() ([]byte, error) {
	return emap.MergeMarshal(h.EssentialHeader, h.PrivateParams)
}

func (h *Header) UnmarshalJSON(data []byte) error {
	if h.EssentialHeader == nil {
		h.EssentialHeader = &EssentialHeader{}
	}
	if h.PrivateParams == nil {
		h.PrivateParams = map[string]interface{}{}
	}
	return emap.MergeUnmarshal(data, h.EssentialHeader, &h.PrivateParams)
}

func (h *EssentialHeader) Construct(m map[string]interface{}) error {
	r := emap.Hmap(m)
	{
		alg, err := r.GetString("alg")
		if err != nil {
			return err
		}
		h.Algorithm = jwa.KeyEncryptionAlgorithm(alg)
	}

	{
		alg, err := r.GetString("enc")
		if err != nil {
			return err
		}
		h.ContentEncryption = jwa.ContentEncryptionAlgorithm(alg)
	}
	h.ContentType, _ = r.GetString("cty")
	h.KeyID, _ = r.GetString("kid")
	h.Type, _ = r.GetString("typ")
	h.X509CertThumbprint, _ = r.GetString("x5t")
	h.X509CertThumbprintS256, _ = r.GetString("x5t#256")
	if v, err := r.GetStringSlice("crit"); err != nil {
		h.Critical = v
	}
	if v, err := r.GetStringSlice("x5c"); err != nil {
		h.X509CertChain = v
	}
	if v, err := r.GetString("jku"); err == nil {
		u, err := url.Parse(v)
		if err == nil {
			h.JwkSetURL = u
		}
	}

	if v, err := r.GetString("x5u"); err == nil {
		u, err := url.Parse(v)
		if err == nil {
			h.X509Url = u
		}
	}

	return nil
}

func Parse(buf []byte) (*Message, error) {
	buf = bytes.TrimSpace(buf)
	if len(buf) == 0 {
		return nil, errors.New("empty buffer")
	}

	if buf[0] == '{' {
		return parseJSON(buf)
	}
	return parseCompact(buf)
}

func ParseString(s string) (*Message, error) {
	return Parse([]byte(s))
}

func parseJSON(buf []byte) (*Message, error) {
	m := struct {
		*Message
		*Recipient
	}{}

	if err := json.Unmarshal(buf, &m); err != nil {
		return nil, err
	}

	// if the "signature" field exist, treat it as a flattened
	if m.Recipient != nil {
		if len(m.Message.Recipients) != 0 {
			return nil, errors.New("invalid message: mixed flattened/full json serialization")
		}

		m.Message.Recipients = []Recipient{*m.Recipient}
	}

	return m.Message, nil
}

func parseCompact(buf []byte) (*Message, error) {
	parts := bytes.Split(buf, []byte{'.'})
	if len(parts) != 5 {
		return nil, ErrInvalidCompactPartsCount
	}

	enc := base64.RawURLEncoding
	p0Len := enc.DecodedLen(len(parts[0]))
	p1Len := enc.DecodedLen(len(parts[1]))
	p2Len := enc.DecodedLen(len(parts[2]))
	p3Len := enc.DecodedLen(len(parts[3]))
	p4Len := enc.DecodedLen(len(parts[4]))

	out := make([]byte, p0Len+p1Len+p2Len+p3Len+p4Len)

	hdrbuf := buffer.Buffer(out[:p0Len])
	if _, err := enc.Decode(hdrbuf, parts[0]); err != nil {
		return nil, err
	}
	hdrbuf = bytes.TrimRight(hdrbuf, "\x00")

	hdr := NewHeader()
	if err := json.Unmarshal(hdrbuf, hdr); err != nil {
		return nil, err
	}

	enckeybuf := buffer.Buffer(out[p0Len : p0Len+p1Len])
	if _, err := enc.Decode(enckeybuf, parts[1]); err != nil {
		return nil, err
	}
	enckeybuf = bytes.TrimRight(enckeybuf, "\x00")

	ivbuf := buffer.Buffer(out[p0Len+p1Len : p0Len+p1Len+p2Len])
	if _, err := enc.Decode(ivbuf, parts[2]); err != nil {
		return nil, err
	}
	ivbuf = bytes.TrimRight(ivbuf, "\x00")

	ctbuf := buffer.Buffer(out[p0Len+p1Len+p2Len : p0Len+p1Len+p2Len+p3Len])
	if _, err := enc.Decode(ctbuf, parts[3]); err != nil {
		return nil, err
	}
	ctbuf = bytes.TrimRight(ctbuf, "\x00")

	tagbuf := buffer.Buffer(out[p0Len+p1Len+p2Len+p3Len : p0Len+p1Len+p2Len+p3Len+p4Len])
	if _, err := enc.Decode(tagbuf, parts[4]); err != nil {
		return nil, err
	}
	tagbuf = bytes.TrimRight(tagbuf, "\x00")

	m := NewMessage()
	m.Tag = tagbuf
	m.CipherText = ctbuf
	m.InitializationVector = ivbuf
	m.Recipients = []Recipient{
		Recipient{
			Header:       *hdr,
			EncryptedKey: enckeybuf,
		},
	}
	return m, nil
}
