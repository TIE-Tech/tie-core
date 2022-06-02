package vrf

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"errors"
	tcrypto "github.com/tie-core/common/crypto"
	"hash"
	"math/big"
)

var (
	ErrKeyNotSupported = errors.New("only support ECC key")
	ErrEvalVRF         = errors.New("failed to evaluate vrf")
)

//Vrf returns the verifiable random function evaluated m and a NIZK proof
func Vrf(prv *ecdsa.PrivateKey, msg []byte) (vrf, nizk []byte, err error) {
	isValid := ValidatePrivateKey(prv)
	if !isValid {
		return nil, nil, ErrKeyNotSupported
	}

	h := getHash(prv.Curve)
	byteLen := (prv.Params().BitSize + 7) >> 3
	_, proof := Evaluate(prv, h, msg)
	if proof == nil {
		return nil, nil, ErrEvalVRF
	}

	nizk = proof[0 : 2*byteLen]
	vrf = proof[2*byteLen : 2*byteLen+2*byteLen+1]
	err = nil
	return
}

//Verify returns true if vrf and nizk is correct for msg
func Verify(pub *ecdsa.PublicKey, msg, vrf, nizk []byte) (bool, error) {
	isValid := ValidatePublicKey(pub)
	if !isValid {
		return false, ErrKeyNotSupported
	}

	h := getHash(pub.Curve)
	byteLen := (pub.Params().BitSize + 7) >> 3
	if len(vrf) != byteLen*2+1 || len(nizk) != byteLen*2 {
		return false, nil
	}
	proof := append(nizk, vrf...)
	_, err := ProofToHash(pub, h, msg, proof)
	if err != nil {
		return false, nil
	}
	return true, nil
}

/*
 * ValidatePrivateKey checks two conditions:
 *  - the private key must be of type ec.PrivateKey
 *	- the private key must use curve secp256r1
 */
func ValidatePrivateKey(prv *ecdsa.PrivateKey) bool {
	h := getHash(prv.Curve)
	if h == nil {
		return false
	}
	return true
}

/*
 * ValidatePublicKey checks two conditions:
 *  - the public key must be of type ec.PublicKey
 *	- the public key must use curve secp256r1
 */
func ValidatePublicKey(pub *ecdsa.PublicKey) bool {
	h := getHash(pub.Curve)
	if h == nil {
		return false
	}
	return true
}

func getHash(curve elliptic.Curve) hash.Hash {
	bitSize := curve.Params().BitSize
	switch bitSize {
	case 224:
		return crypto.SHA224.New()
	case 256:
		switch curve.Params().Name {

		case "P-256", tcrypto.S256.Name:
			return crypto.SHA256.New()
		default:
			return nil
		}
	case 384:
		return crypto.SHA384.New()
	default:
		return nil
	}
}

// HashToBigInt Convert hash value to big integer
func HashToBigInt(hash []byte) *big.Int {
	orderBits := tcrypto.S256.Params().N.BitLen()
	orderBytes := (orderBits + 7) / 8
	if len(hash) > orderBytes {
		hash = hash[:orderBytes]
	}

	ret := new(big.Int).SetBytes(hash)
	excess := len(hash)*8 - orderBits
	if excess > 0 {
		ret.Rsh(ret, uint(excess))
	}
	return ret
}