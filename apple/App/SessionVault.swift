import Foundation
import Security
import YTMDLCore

enum SessionVault {
    private static let service = "org.ytmdl.player.session"
    private static func query(_ origin: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service, kSecAttrAccount as String: origin]
    }
    static func read(_ origin: String) throws -> Data? {
        var attributes = query(origin)
        attributes[kSecReturnData as String] = true
        attributes[kSecMatchLimit as String] = kSecMatchLimitOne
        var value: CFTypeRef?
        let status = SecItemCopyMatching(attributes as CFDictionary, &value)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess else { throw VaultError.unavailable }
        return value as? Data
    }
    static func write(_ data: Data, origin: String) throws {
        let attributes = query(origin)
        let update: [String: Any] = [kSecValueData as String: data]
        var status = SecItemUpdate(attributes as CFDictionary, update as CFDictionary)
        if status == errSecItemNotFound {
            var addition = attributes
            addition[kSecValueData as String] = data
            addition[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            addition[kSecAttrSynchronizable as String] = false
            status = SecItemAdd(addition as CFDictionary, nil)
        }
        guard status == errSecSuccess else { throw VaultError.unavailable }
    }
    static func remove(_ origin: String) throws {
        let status = SecItemDelete(query(origin) as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw VaultError.unavailable }
    }
}
enum VaultError: LocalizedError {
    case unavailable
    var errorDescription: String? { "Der Schlüsselbund ist nicht erreichbar. Die Sitzung wurde nicht dauerhaft gespeichert." }
}

struct StoredCookie: Codable {
    let name: String
    let value: String
    let expiresAt: Date?
    let secure: Bool?

    init(name: String, value: String, expiresAt: Date?, secure: Bool? = nil) {
        self.name = name; self.value = value; self.expiresAt = expiresAt; self.secure = secure
    }
    func cookie(for server: ServerAddress, now: Date = Date()) -> HTTPCookie? {
        guard ["ytmdl_session", "ytmdl_csrf"].contains(name), let expiresAt, expiresAt > now else { return nil }
        var properties: [HTTPCookiePropertyKey: Any] = [.name: name, .value: value,
            .domain: server.url.host!, .path: "/", .expires: expiresAt, .originURL: server.url]
        // Foundation treats any present Secure string (even "FALSE") as true.
        // Preserve the original flag; old records infer it from their bound origin.
        if server.isSecure || secure == true { properties[.secure] = "TRUE" }
        return HTTPCookie(properties: properties)
    }
}
