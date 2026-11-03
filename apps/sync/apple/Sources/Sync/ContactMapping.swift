import Contacts
import Foundation

/// A `CNContact` as providers.substrate.reamde.dev/apple/contact declares
/// it; the apple README is the contract.
enum ContactMapping {
    /// Every key the contact kind declares, plus the formatter's full name.
    /// No note (it needs an entitlement) and no image.
    static var keys: [CNKeyDescriptor] {
        let plain: [String] = [
            CNContactIdentifierKey, CNContactTypeKey, CNContactNamePrefixKey, CNContactGivenNameKey,
            CNContactMiddleNameKey, CNContactFamilyNameKey, CNContactPreviousFamilyNameKey,
            CNContactNameSuffixKey, CNContactNicknameKey, CNContactPhoneticGivenNameKey,
            CNContactPhoneticMiddleNameKey, CNContactPhoneticFamilyNameKey, CNContactOrganizationNameKey,
            CNContactPhoneticOrganizationNameKey, CNContactDepartmentNameKey, CNContactJobTitleKey,
            CNContactBirthdayKey, CNContactDatesKey, CNContactPhoneNumbersKey, CNContactEmailAddressesKey,
            CNContactPostalAddressesKey, CNContactUrlAddressesKey, CNContactSocialProfilesKey,
            CNContactInstantMessageAddressesKey, CNContactRelationsKey,
        ]
        return plain.map { $0 as CNKeyDescriptor } + [CNContactFormatter.descriptorForRequiredKeys(for: .fullName)]
    }

    /// The row's properties. `addressbook` is the reference every contact
    /// carries; everything empty is left out.
    static func properties(_ c: CNContact, addressbook: String) -> JSON {
        var f = Fields()
        f.set("addressbook", reference("providers.substrate.reamde.dev/apple/addressbook", addressbook))
        f.set("identifier", c.identifier)
        f.set("contactType", c.contactType == .organization ? "organization" : "person")
        f.set("namePrefix", c.namePrefix)
        f.set("givenName", c.givenName)
        f.set("middleName", c.middleName)
        f.set("familyName", c.familyName)
        f.set("previousFamilyName", c.previousFamilyName)
        f.set("nameSuffix", c.nameSuffix)
        f.set("nickname", c.nickname)
        f.set("phoneticGivenName", c.phoneticGivenName)
        f.set("phoneticMiddleName", c.phoneticMiddleName)
        f.set("phoneticFamilyName", c.phoneticFamilyName)
        f.set("organizationName", c.organizationName)
        f.set("phoneticOrganizationName", c.phoneticOrganizationName)
        f.set("departmentName", c.departmentName)
        f.set("jobTitle", c.jobTitle)
        f.set("displayName", CNContactFormatter.string(from: c, style: .fullName))
        if let birthday = c.birthday { f.set("birthday", dateComponents(birthday, label: nil)) }
        f.set("dates", .array(c.dates.compactMap { dateComponents($0.value as DateComponents, label: $0.label) }))
        f.set("phoneNumbers", .array(c.phoneNumbers.compactMap { entry in
            labeled(entry.label) { $0.set("value", entry.value.stringValue) }
        }))
        f.set("emailAddresses", .array(c.emailAddresses.compactMap { entry in
            let address = entry.value as String
            guard isMailbox(address) else { return nil }
            return labeled(entry.label) { $0.set("value", address) }
        }))
        f.set("postalAddresses", .array(c.postalAddresses.compactMap { entry in
            let a = entry.value
            return labeled(entry.label) {
                $0.set("street", a.street)
                $0.set("subLocality", a.subLocality)
                $0.set("city", a.city)
                $0.set("subAdministrativeArea", a.subAdministrativeArea)
                $0.set("state", a.state)
                $0.set("postalCode", a.postalCode)
                $0.set("country", a.country)
                $0.set("isoCountryCode", a.isoCountryCode)
            }
        }))
        f.set("urlAddresses", .array(c.urlAddresses.compactMap { entry in
            labeled(entry.label) { $0.set("value", entry.value as String) }
        }))
        f.set("socialProfiles", .array(c.socialProfiles.compactMap { entry in
            let p = entry.value
            return labeled(entry.label) {
                $0.set("urlString", p.urlString)
                $0.set("username", p.username)
                $0.set("userIdentifier", p.userIdentifier)
                $0.set("service", p.service)
            }
        }))
        f.set("instantMessageAddresses", .array(c.instantMessageAddresses.compactMap { entry in
            let im = entry.value
            return labeled(entry.label) {
                $0.set("username", im.username)
                $0.set("service", im.service)
            }
        }))
        f.set("contactRelations", .array(c.contactRelations.compactMap { entry in
            labeled(entry.label) { $0.set("name", entry.value.name) }
        }))
        return f.json
    }

    /// An entry with its label, or nil when nothing but the label is set: an
    /// entry whose value is empty is not an entry.
    private static func labeled(_ label: String?, _ fill: (inout Fields) -> Void) -> JSON? {
        var inner = Fields()
        fill(&inner)
        guard !inner.pairs.isEmpty else { return nil }
        var f = Fields()
        f.set("label", label.map(englishLabel))
        return .object(f.pairs + inner.pairs)
    }

    private static func dateComponents(_ d: DateComponents, label: String?) -> JSON? {
        guard let month = d.month, let day = d.day, month != NSDateComponentUndefined,
              day != NSDateComponentUndefined
        else { return nil }
        var f = Fields()
        f.set("label", label.map(englishLabel))
        if let year = d.year, year != NSDateComponentUndefined, year >= 1 { f.set("year", year) }
        f.set("month", month)
        f.set("day", day)
        return f.json
    }

    /// Apple's built-in labels as the English words the kind stores, whatever
    /// the device's language: `_$!<Mobile>!$_` is "mobile" and
    /// `_$!<HomeFAX>!$_` "home fax". A custom label is kept as typed.
    static func englishLabel(_ label: String) -> String {
        guard label.hasPrefix("_$!<"), label.hasSuffix(">!$_"), label.count > 8 else { return label }
        let word = String(label.dropFirst(4).dropLast(4))
        if let known = builtIn[word] { return known }
        // CamelCase into words, an upper-case run kept as one: HomeFAX is
        // "home fax".
        var words: [String] = []
        var current = ""
        let chars = Array(word)
        for (i, ch) in chars.enumerated() {
            let startsWord = ch.isUppercase && !current.isEmpty
                && (!(chars[i - 1].isUppercase) || (i + 1 < chars.count && chars[i + 1].isLowercase))
            if startsWord {
                words.append(current)
                current = ""
            }
            current.append(ch)
        }
        if !current.isEmpty { words.append(current) }
        return words.map { $0.lowercased() }.joined(separator: " ")
    }

    /// Labels whose word is not their CamelCase split.
    private static let builtIn: [String: String] = [
        "HomePage": "homepage",
    ]

    /// Whether Go's `net/mail.ParseAddress` accepts the address as it
    /// stands, held to the plain `local@domain` form: an address the server
    /// would refuse refuses the whole contact, so anything doubtful is left
    /// out rather than sent.
    static func isMailbox(_ address: String) -> Bool {
        let parts = address.split(separator: "@", omittingEmptySubsequences: false)
        guard parts.count == 2 else { return false }
        return isDotAtom(parts[0]) && isDotAtom(parts[1])
    }

    private static func isDotAtom(_ s: Substring) -> Bool {
        guard !s.isEmpty, !s.hasPrefix("."), !s.hasSuffix("."), !s.contains("..") else { return false }
        return s.unicodeScalars.allSatisfy { c in
            if c == "." { return true }
            if c.value >= 0x80 { return true }
            if c.value <= 0x20 || c.value == 0x7f { return false }
            return !"()<>[]:;@\\,\"".unicodeScalars.contains(c)
        }
    }
}

/// A stored reference value: `{ref: <kind>/<id>}`.
func reference(_ kind: String, _ id: String) -> JSON {
    .object([("ref", .string(kind + "/" + id))])
}
