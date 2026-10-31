// Sparkle update signing without Sparkle's tools: an Ed25519 key, kept as
// the base64 of its 32-byte seed (the SPARKLE_PRIVATE_KEY secret, also what
// Sparkle's own generate_keys -x writes).
//
//   swift sparkle.swift new-key          prints a new private key
//   swift sparkle.swift public-key       prints the public key (SUPublicEDKey)
//   swift sparkle.swift sign <file>      prints the update's sparkle:edSignature
//
// public-key and sign read the private key from SPARKLE_PRIVATE_KEY.
import CryptoKit
import Foundation

func fail(_ message: String) -> Never {
    FileHandle.standardError.write(Data((message + "\n").utf8))
    exit(1)
}

func privateKey() -> Curve25519.Signing.PrivateKey {
    let text = (ProcessInfo.processInfo.environment["SPARKLE_PRIVATE_KEY"] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
    guard let data = Data(base64Encoded: text), data.count >= 32,
          let key = try? Curve25519.Signing.PrivateKey(rawRepresentation: data.prefix(32)) else {
        fail("SPARKLE_PRIVATE_KEY isn't a base64 Ed25519 key")
    }
    return key
}

let args = CommandLine.arguments.dropFirst()
switch args.first {
case "new-key":
    print(Curve25519.Signing.PrivateKey().rawRepresentation.base64EncodedString())
case "public-key":
    print(privateKey().publicKey.rawRepresentation.base64EncodedString())
case "sign":
    guard args.count == 2, let data = FileManager.default.contents(atPath: args[args.startIndex + 1]) else { fail("usage: sign <file>") }
    let key = privateKey()
    let signature = try key.signature(for: data)
    guard key.publicKey.isValidSignature(signature, for: data) else { fail("signature didn't verify") }
    print(signature.base64EncodedString())
default:
    fail("usage: sparkle.swift new-key | public-key | sign <file>")
}
