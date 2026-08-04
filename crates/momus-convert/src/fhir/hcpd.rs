//! HCPD/AU-specific FHIR generation overrides.
//!
//! Ported from fhir-autotest's `generate::hcpd` module. Provides:
//! 1. Australian locality data (suburbs with postcodes and states)
//! 2. AU-specific identifier generation (HPI-I, HPI-O, ABN, Medicare)
//! 3. Post-processing fixes for AU compliance on generated resources
//! 4. Detection of HCPD/AU implementation guides

use rand::Rng;
use std::collections::HashMap;

// ── Australian locality data ──────────────────────────────────────────────

/// An Australian suburb/locality with its postcode and state.
pub struct AustralianSuburb {
    pub name: &'static str,
    pub postcode: &'static str,
    pub state: &'static str,
}

/// A curated list of Australian suburbs across all states and territories.
/// These are commonly used in healthcare provider addresses.
pub const AUSTRALIAN_SUBURBS: &[AustralianSuburb] = &[
    // New South Wales
    AustralianSuburb { name: "Sydney", postcode: "2000", state: "NSW" },
    AustralianSuburb { name: "Surry Hills", postcode: "2010", state: "NSW" },
    AustralianSuburb { name: "Darlinghurst", postcode: "2010", state: "NSW" },
    AustralianSuburb { name: "Parramatta", postcode: "2150", state: "NSW" },
    AustralianSuburb { name: "Newcastle", postcode: "2300", state: "NSW" },
    AustralianSuburb { name: "Wollongong", postcode: "2500", state: "NSW" },
    AustralianSuburb { name: "Chatswood", postcode: "2067", state: "NSW" },
    AustralianSuburb { name: "Bondi Junction", postcode: "2022", state: "NSW" },
    AustralianSuburb { name: "North Sydney", postcode: "2060", state: "NSW" },
    AustralianSuburb { name: "Penrith", postcode: "2750", state: "NSW" },
    AustralianSuburb { name: "Gosford", postcode: "2250", state: "NSW" },
    AustralianSuburb { name: "Liverpool", postcode: "2170", state: "NSW" },
    AustralianSuburb { name: "Campbelltown", postcode: "2560", state: "NSW" },
    AustralianSuburb { name: "Tamworth", postcode: "2340", state: "NSW" },
    AustralianSuburb { name: "Dubbo", postcode: "2830", state: "NSW" },
    // Victoria
    AustralianSuburb { name: "Melbourne", postcode: "3000", state: "VIC" },
    AustralianSuburb { name: "Fitzroy", postcode: "3065", state: "VIC" },
    AustralianSuburb { name: "Richmond", postcode: "3121", state: "VIC" },
    AustralianSuburb { name: "St Kilda", postcode: "3182", state: "VIC" },
    AustralianSuburb { name: "Geelong", postcode: "3220", state: "VIC" },
    AustralianSuburb { name: "Ballarat", postcode: "3350", state: "VIC" },
    AustralianSuburb { name: "Bendigo", postcode: "3550", state: "VIC" },
    AustralianSuburb { name: "Dandenong", postcode: "3175", state: "VIC" },
    AustralianSuburb { name: "Preston", postcode: "3072", state: "VIC" },
    AustralianSuburb { name: "Footscray", postcode: "3011", state: "VIC" },
    AustralianSuburb { name: "Box Hill", postcode: "3128", state: "VIC" },
    AustralianSuburb { name: "Frankston", postcode: "3199", state: "VIC" },
    // Queensland
    AustralianSuburb { name: "Brisbane", postcode: "4000", state: "QLD" },
    AustralianSuburb { name: "South Brisbane", postcode: "4101", state: "QLD" },
    AustralianSuburb { name: "Fortitude Valley", postcode: "4006", state: "QLD" },
    AustralianSuburb { name: "Gold Coast", postcode: "4217", state: "QLD" },
    AustralianSuburb { name: "Sunshine Coast", postcode: "4558", state: "QLD" },
    AustralianSuburb { name: "Townsville", postcode: "4810", state: "QLD" },
    AustralianSuburb { name: "Cairns", postcode: "4870", state: "QLD" },
    AustralianSuburb { name: "Toowoomba", postcode: "4350", state: "QLD" },
    AustralianSuburb { name: "Ipswich", postcode: "4305", state: "QLD" },
    AustralianSuburb { name: "Rockhampton", postcode: "4700", state: "QLD" },
    // Western Australia
    AustralianSuburb { name: "Perth", postcode: "6000", state: "WA" },
    AustralianSuburb { name: "Fremantle", postcode: "6160", state: "WA" },
    AustralianSuburb { name: "Subiaco", postcode: "6008", state: "WA" },
    AustralianSuburb { name: "Joondalup", postcode: "6027", state: "WA" },
    AustralianSuburb { name: "Bunbury", postcode: "6230", state: "WA" },
    AustralianSuburb { name: "Albany", postcode: "6330", state: "WA" },
    AustralianSuburb { name: "Geraldton", postcode: "6530", state: "WA" },
    AustralianSuburb { name: "Mandurah", postcode: "6210", state: "WA" },
    // South Australia
    AustralianSuburb { name: "Adelaide", postcode: "5000", state: "SA" },
    AustralianSuburb { name: "North Adelaide", postcode: "5006", state: "SA" },
    AustralianSuburb { name: "Glenelg", postcode: "5045", state: "SA" },
    AustralianSuburb { name: "Mount Gambier", postcode: "5290", state: "SA" },
    AustralianSuburb { name: "Whyalla", postcode: "5600", state: "SA" },
    AustralianSuburb { name: "Port Augusta", postcode: "5700", state: "SA" },
    // Tasmania
    AustralianSuburb { name: "Hobart", postcode: "7000", state: "TAS" },
    AustralianSuburb { name: "Launceston", postcode: "7250", state: "TAS" },
    AustralianSuburb { name: "Devonport", postcode: "7310", state: "TAS" },
    AustralianSuburb { name: "Burnie", postcode: "7320", state: "TAS" },
    // Australian Capital Territory
    AustralianSuburb { name: "Canberra", postcode: "2600", state: "ACT" },
    AustralianSuburb { name: "Belconnen", postcode: "2617", state: "ACT" },
    AustralianSuburb { name: "Woden", postcode: "2606", state: "ACT" },
    AustralianSuburb { name: "Tuggeranong", postcode: "2900", state: "ACT" },
    // Northern Territory
    AustralianSuburb { name: "Darwin", postcode: "0800", state: "NT" },
    AustralianSuburb { name: "Alice Springs", postcode: "0870", state: "NT" },
    AustralianSuburb { name: "Palmerston", postcode: "0830", state: "NT" },
    AustralianSuburb { name: "Katherine", postcode: "0850", state: "NT" },
];

/// Pick a random Australian suburb from the curated list.
pub fn random_australian_suburb(rng: &mut impl Rng) -> &'static AustralianSuburb {
    &AUSTRALIAN_SUBURBS[rng.random_range(0..AUSTRALIAN_SUBURBS.len())]
}

// ── HCPD IG detection ─────────────────────────────────────────────────────

/// Return true when the profile URLs in a loaded IG indicate an HCPD/AU
/// implementation guide, so that HCPD-specific identifier and extension
/// overrides are applied only when appropriate.
pub fn is_hcpd_ig(profile_urls: &HashMap<String, String>) -> bool {
    profile_urls.values().any(|url| {
        url.contains("digitalhealth.gov.au") || url.contains("/hcpd/") || url.contains("hl7.org.au")
    })
}

// ── Identifier generation ──────────────────────────────────────────────────

/// Generate a random digit string of the given length.
/// The first digit is never zero.
pub fn random_digits(len: usize, rng: &mut impl Rng) -> String {
    let mut out = String::with_capacity(len);
    for i in 0..len {
        let d: u8 = if i == 0 {
            rng.random_range(1..10)
        } else {
            rng.random_range(0..10)
        };
        out.push(char::from(b'0' + d));
    }
    out
}

/// Generate a Luhn-checked identifier with a fixed prefix.
///
/// The prefix is prepended, then random digits fill the remaining positions
/// (minus one for the check digit), and a Luhn check digit is appended.
/// The total length of the returned string is `total_len`.
pub fn luhn_with_prefix(prefix: &str, total_len: usize, rng: &mut impl Rng) -> String {
    let payload_len = total_len.saturating_sub(1);
    let mut base = prefix.to_string();
    while base.len() < payload_len {
        base.push(char::from(b'0' + rng.random_range(0..10)));
    }
    base.truncate(payload_len);

    let mut sum = 0u32;
    let mut double = true;
    for ch in base.chars().rev() {
        let mut n = ch.to_digit(10).unwrap_or(0);
        if double {
            n *= 2;
            if n > 9 {
                n -= 9;
            }
        }
        sum += n;
        double = !double;
    }
    let check = (10 - (sum % 10)) % 10;
    format!("{}{}", base, check)
}

/// Generate an HPI-I (Healthcare Provider Identifier - Individual) value.
/// HPI-I is 16 digits, starts with "800361", and has a Luhn check digit.
pub fn generate_hpii(rng: &mut impl Rng) -> String {
    luhn_with_prefix("800361", 16, rng)
}

/// Generate an HPI-O (Healthcare Provider Identifier - Organisation) value.
/// HPI-O is 16 digits, starts with "800362", and has a Luhn check digit.
pub fn generate_hpio(rng: &mut impl Rng) -> String {
    luhn_with_prefix("800362", 16, rng)
}

/// Generate an ABN (Australian Business Number) value.
/// ABN is 11 digits with a specific weighting checksum.
pub fn generate_abn(rng: &mut impl Rng) -> String {
    // ABN: 11 digits. The first digit is always 1 less than the checksum
    // would produce (ABN uses a modified check). For synthetic data we
    // generate 11 random digits as a valid-looking ABN.
    random_digits(11, rng)
}

/// Generate a Medicare registration number (MED followed by 10 digits).
pub fn generate_medicare_registration(rng: &mut impl Rng) -> String {
    format!("MED{}", random_digits(10, rng))
}

// ── Post-processing fixes ──────────────────────────────────────────────────

/// Extract the resource ID from a reference string like "Organization/org-1".
fn extract_reference_id(reference: &str) -> Option<&str> {
    reference.split_once('/').map(|(_, id)| id)
}

/// Apply HCPD/AU-specific overrides to a generated resource.
///
/// This function is ONLY called when `hcpd_ig` is true (i.e. the loaded IG
/// package is the HCPD/AU IG). For all other IGs, the profile-aware generator
/// produces conformant resources without needing IG-specific identifier
/// augmentation.
pub fn apply_hcpd_bulk_fixes(
    resource: &mut serde_json::Value,
    resource_type: &str,
    id: &str,
    practitioner_registration_by_id: &mut HashMap<String, String>,
    value_set_systems: &HashMap<String, String>,
    code_system_codes: &HashMap<String, (String, Option<String>)>,
    rng: &mut impl Rng,
) {
    match resource_type {
        "Organization" => {
            resource["identifier"] = serde_json::json!([
                {
                    "system": "http://hl7.org.au/id/abn",
                    "type": {
                        "coding": [
                            {
                                "system": "http://terminology.hl7.org/CodeSystem/v2-0203",
                                "code": "TAX"
                            }
                        ]
                    },
                    "value": generate_abn(rng)
                },
                {
                    "system": "http://ns.electronichealth.net.au/id/hi/hpio/1.0",
                    "type": {
                        "coding": [
                            {
                                "system": "http://terminology.hl7.org.au/CodeSystem/v2-0203",
                                "code": "NOI"
                            }
                        ]
                    },
                    "extension": [
                        {
                            "url": "http://digitalhealth.gov.au/fhir/hcpd/StructureDefinition/hi-org-classification",
                            "valueCodeableConcept": {
                                "coding": [
                                    {
                                        "system": "http://digitalhealth.gov.au/fhir/hcpd/CodeSystem/hi-org-classification-cs",
                                        "code": "seed",
                                        "display": "Seed"
                                    }
                                ]
                            }
                        }
                    ],
                    "value": generate_hpio(rng)
                }
            ]);

            if resource.get("address").is_none() || !resource["address"].is_array() {
                resource["address"] = serde_json::json!([{}]);
            }
            if let Some(first_addr) = resource
                .get_mut("address")
                .and_then(|a| a.as_array_mut())
                .and_then(|a| a.first_mut())
            {
                if first_addr.get("type").is_none() {
                    first_addr["type"] = serde_json::Value::String("physical".to_string());
                }
                if first_addr.get("line").is_none() {
                    first_addr["line"] = serde_json::json!(["100 George St"]);
                }
                if first_addr.get("city").is_none() {
                    first_addr["city"] = serde_json::Value::String("Sydney".to_string());
                }
                if first_addr.get("state").is_none() {
                    first_addr["state"] = serde_json::Value::String("NSW".to_string());
                }
                if first_addr.get("postalCode").is_none() {
                    first_addr["postalCode"] = serde_json::Value::String("2000".to_string());
                }
                if first_addr.get("country").is_none() {
                    first_addr["country"] = serde_json::Value::String("AU".to_string());
                }
            }
        }
        "Practitioner" => {
            resource["identifier"] = serde_json::json!([
                {
                    "system": "http://ns.electronichealth.net.au/id/hi/hpii/1.0",
                    "type": {
                        "coding": [
                            {
                                "system": "http://terminology.hl7.org/CodeSystem/v2-0203",
                                "code": "NPI"
                            }
                        ]
                    },
                    "value": generate_hpii(rng)
                }
            ]);

            let registration_number = generate_medicare_registration(rng);
            resource["qualification"] = serde_json::json!([
                {
                    "code": {
                        "text": "General practice"
                    },
                    "identifier": [
                        {
                            "system": "http://hl7.org.au/id/ahpra-registration-number",
                            "type": {
                                "coding": [
                                    {
                                        "system": "http://terminology.hl7.org.au/CodeSystem/v2-0203",
                                        "code": "AHPRA"
                                    }
                                ]
                            },
                            "value": registration_number
                        }
                    ],
                    "issuer": {
                        "reference": "Organization/organization-1"
                    }
                }
            ]);

            resource["extension"] = serde_json::json!([
                {
                    "url": "http://hl7.org/fhir/StructureDefinition/individual-recordedSexOrGender",
                    "extension": [
                        {
                            "url": "value",
                            "valueCodeableConcept": {
                                "coding": [
                                    {
                                        "system": "http://hl7.org/fhir/administrative-gender",
                                        "code": "male",
                                        "display": "Male"
                                    }
                                ]
                            }
                        }
                    ]
                }
            ]);

            practitioner_registration_by_id.insert(id.to_string(), registration_number);
        }
        "HealthcareService" => {
            if resource.get("type").is_none() || !resource["type"].is_array() {
                resource["type"] = serde_json::json!([{}]);
            }
            if let Some(first_type) = resource
                .get_mut("type")
                .and_then(|a| a.as_array_mut())
                .and_then(|a| a.first_mut())
            {
                // Always set a valid SNOMED coding with display — the HCPD profile
                // requires type.coding.display (min = 1).
                first_type["coding"] = serde_json::json!([
                    {
                        "system": "http://snomed.info/sct",
                        "code": "408443003",
                        "display": "General medical practice"
                    }
                ]);
            }

            // Fix suppressedBy extension coding — the HCPD profile requires a code
            // from the responsible-party-type ValueSet, not NullFlavor.
            fix_suppressed_by_coding(resource, value_set_systems, code_system_codes);

            // Fix serviceProvisionCode — the profile-aware generator uses code
            // "unknown" which doesn't exist in the HCPD service-provision CodeSystem.
            // Replace with the first valid code from the CodeSystem.
            fix_service_provision_code(resource, code_system_codes);
        }
        "Location" => {
            resource["type"] = serde_json::json!([
                {
                    "text": "Healthcare service location"
                }
            ]);
        }
        "PractitionerRole" => {
            let practitioner_id = resource
                .get("practitioner")
                .and_then(|p| p.get("reference"))
                .and_then(|r| r.as_str())
                .and_then(extract_reference_id);

            let registration_number = practitioner_id
                .and_then(|pid| practitioner_registration_by_id.get(pid).cloned())
                .unwrap_or_else(|| generate_medicare_registration(rng));

            resource["identifier"] = serde_json::json!([
                {
                    "system": "http://digitalhealth.gov.au/fhir/hcpd/id/hcpd-local-identifier",
                    "type": {
                        "coding": [
                            {
                                "system": "http://terminology.hl7.org/CodeSystem/v2-0203",
                                "code": "XX"
                            }
                        ]
                    },
                    "value": random_digits(12, rng)
                },
                {
                    "system": "http://hl7.org.au/id/ahpra-registration-number",
                    "type": {
                        "coding": [
                            {
                                "system": "http://terminology.hl7.org.au/CodeSystem/v2-0203",
                                "code": "AHPRA"
                            }
                        ]
                    },
                    "value": registration_number
                }
            ]);

            // Fix suppressedBy extension coding — the HCPD profile requires a code
            // from the responsible-party-type ValueSet, not NullFlavor.
            fix_suppressed_by_coding(resource, value_set_systems, code_system_codes);
        }
        _ => {}
    }
}

/// Fix the `suppressedBy.valueCodeableConcept.coding` in the `suppressed` extension
/// to use a valid code from the responsible-party-type CodeSystem.
///
/// The code is looked up from `value_set_systems` (ValueSet URL → system URL) and
/// `code_system_codes` (system URL → first valid code), falling back to `"UNK"` if
/// neither map contains the relevant entries. This avoids any hardcoded HCPD codes.
fn fix_suppressed_by_coding(
    resource: &mut serde_json::Value,
    value_set_systems: &HashMap<String, String>,
    code_system_codes: &HashMap<String, (String, Option<String>)>,
) {
    // Find the system URL bound to the suppressedBy coding
    // (typically via responsible-party-type ValueSet in HCPD)
    let vs_url = "http://digitalhealth.gov.au/fhir/cc/ValueSet/responsible-party-type";
    let system = value_set_systems
        .get(vs_url)
        .map(|s| s.as_str())
        .unwrap_or("http://digitalhealth.gov.au/fhir/cc/CodeSystem/responsible-party-type");
    let (code, display) = code_system_codes
        .get(system)
        .map(|(c, d)| (c.as_str(), d.as_deref().unwrap_or(c.as_str())))
        .unwrap_or(("UNK", "Unknown"));

    let Some(exts) = resource.get_mut("extension").and_then(|e| e.as_array_mut()) else {
        return;
    };
    for ext in exts {
        let url = ext.get("url").and_then(|u| u.as_str()).unwrap_or("");
        if !url.contains("suppressed") {
            continue;
        }
        let Some(sub_exts) = ext.get_mut("extension").and_then(|e| e.as_array_mut()) else {
            continue;
        };
        for sub_ext in sub_exts.iter_mut() {
            let sub_url = sub_ext.get("url").and_then(|u| u.as_str()).unwrap_or("");
            if sub_url == "suppressedBy" {
                // Only override if the current coding is the generic NullFlavor fallback.
                // When populate_extension_slices has already applied a fixedCoding from the
                // profile (e.g. organisation-initiated for Organization/HealthcareService),
                // leave it intact.
                let already_valid = sub_ext
                    .get("valueCodeableConcept")
                    .and_then(|v| v.get("coding"))
                    .and_then(|c| c.as_array())
                    .and_then(|a| a.first())
                    .and_then(|c| c.get("system"))
                    .and_then(|s| s.as_str())
                    .map(|s| !s.contains("NullFlavor"))
                    .unwrap_or(false);
                if already_valid {
                    continue;
                }
                sub_ext["valueCodeableConcept"] = serde_json::json!({
                    "coding": [{
                        "system": system,
                        "code": code
                    }],
                    "text": display
                });
            }
        }
    }
}

/// Fix `serviceProvisionCode` on a HealthcareService resource.
///
/// The profile-aware generator uses code "unknown" which doesn't exist in the
/// HCPD service-provision CodeSystem. Replace it with the first valid code
/// from the CodeSystem, falling back to a hardcoded valid code if the map
/// doesn't contain the system.
fn fix_service_provision_code(
    resource: &mut serde_json::Value,
    code_system_codes: &HashMap<String, (String, Option<String>)>,
) {
    let system = "http://digitalhealth.gov.au/fhir/hcpd/CodeSystem/service-provision-cs";
    let (code, display_str) = code_system_codes
        .get(system)
        .map(|(c, d)| {
            let code: &str = c.as_str();
            let display: &str = d.as_deref().unwrap_or(c.as_str());
            (code.to_string(), display.to_string())
        })
        .unwrap_or(("inperson".to_string(), "In person".to_string()));

    let Some(spc) = resource
        .get_mut("serviceProvisionCode")
        .and_then(|v| v.as_array_mut())
    else {
        return;
    };
    for entry in spc.iter_mut() {
        let Some(codings) = entry.get_mut("coding").and_then(|c| c.as_array_mut()) else {
            continue;
        };
        for coding in codings.iter_mut() {
            let current_code = coding.get("code").and_then(|c| c.as_str());
            if current_code == Some("unknown") || current_code == Some("UNK") {
                coding["code"] = serde_json::Value::String(code.clone());
                coding["display"] = serde_json::Value::String(display_str.clone());
            }
        }
    }
}

// ── AU-specific address generation ────────────────────────────────────────

/// Generate an Australian address for a resource.
/// Uses a random suburb from the curated list.
pub fn generate_au_address(rng: &mut impl Rng) -> serde_json::Value {
    let suburb = random_australian_suburb(rng);
    let street_num = rng.random_range(1..9999);
    let street_names = [
        "George St", "Elizabeth St", "Collins St", "King St", "Queen St",
        "Victoria St", "Albert St", "Edward St", "Park St", "Market St",
        "High St", "Main St", "Smith St", "Brown St", "Station St",
    ];
    let street = street_names[rng.random_range(0..street_names.len())];

    serde_json::json!({
        "type": "physical",
        "line": [format!("{} {}", street_num, street)],
        "city": suburb.name,
        "state": suburb.state,
        "postalCode": suburb.postcode,
        "country": "AU"
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn is_hcpd_ig_detects_digitalhealth_gov_au() {
        let mut urls = HashMap::new();
        urls.insert(
            "Organization".to_string(),
            "http://digitalhealth.gov.au/fhir/hcpd/StructureDefinition/hcpd-Organization"
                .to_string(),
        );
        assert!(is_hcpd_ig(&urls));
    }

    #[test]
    fn is_hcpd_ig_detects_hl7_org_au() {
        let mut urls = HashMap::new();
        urls.insert(
            "Practitioner".to_string(),
            "http://hl7.org.au/fhir/StructureDefinition/au-practitioner".to_string(),
        );
        assert!(is_hcpd_ig(&urls));
    }

    #[test]
    fn is_hcpd_ig_returns_false_for_non_au_igs() {
        let mut urls = HashMap::new();
        urls.insert(
            "Organization".to_string(),
            "http://hl7.org/fhir/us/davinci-pdex-plan-net/StructureDefinition/plannet-Organization"
                .to_string(),
        );
        assert!(!is_hcpd_ig(&urls));
    }

    #[test]
    fn is_hcpd_ig_returns_false_for_empty() {
        let urls = HashMap::new();
        assert!(!is_hcpd_ig(&urls));
    }

    #[test]
    fn random_digits_produces_correct_length() {
        let mut rng = rand::rng();
        let s = random_digits(11, &mut rng);
        assert_eq!(s.len(), 11);
        for (i, c) in s.chars().enumerate() {
            assert!(c.is_ascii_digit(), "char at {} should be a digit", i);
        }
        // First digit should not be zero
        assert_ne!(s.chars().next().unwrap(), '0');
    }

    #[test]
    fn luhn_with_prefix_produces_valid_checksum() {
        let mut rng = rand::rng();
        let s = luhn_with_prefix("800362", 16, &mut rng);
        assert_eq!(s.len(), 16);
        assert!(s.starts_with("800362"));

        // Verify Luhn checksum
        let mut sum = 0u32;
        let mut double = false; // rightmost digit (check digit) is not doubled
        for ch in s.chars().rev() {
            let mut n = ch.to_digit(10).unwrap_or(0);
            if double {
                n *= 2;
                if n > 9 {
                    n -= 9;
                }
            }
            sum += n;
            double = !double;
        }
        assert_eq!(sum % 10, 0, "Luhn checksum should be valid");
    }

    #[test]
    fn extract_reference_id_works() {
        assert_eq!(extract_reference_id("Organization/org-1"), Some("org-1"));
        assert_eq!(
            extract_reference_id("Practitioner/prac-42"),
            Some("prac-42")
        );
        assert_eq!(extract_reference_id("no-slash"), None);
    }

    #[test]
    fn generate_hpii_produces_valid_hpii() {
        let mut rng = rand::rng();
        let hpii = generate_hpii(&mut rng);
        assert_eq!(hpii.len(), 16);
        assert!(hpii.starts_with("800361"));
    }

    #[test]
    fn generate_hpio_produces_valid_hpio() {
        let mut rng = rand::rng();
        let hpio = generate_hpio(&mut rng);
        assert_eq!(hpio.len(), 16);
        assert!(hpio.starts_with("800362"));
    }

    #[test]
    fn generate_abn_produces_11_digits() {
        let mut rng = rand::rng();
        let abn = generate_abn(&mut rng);
        assert_eq!(abn.len(), 11);
        assert!(abn.chars().all(|c| c.is_ascii_digit()));
    }

    #[test]
    fn generate_medicare_registration_produces_med_prefix() {
        let mut rng = rand::rng();
        let reg = generate_medicare_registration(&mut rng);
        assert!(reg.starts_with("MED"));
        assert_eq!(reg.len(), 13); // "MED" + 10 digits
    }

    #[test]
    fn random_australian_suburb_returns_valid_entry() {
        let mut rng = rand::rng();
        let suburb = random_australian_suburb(&mut rng);
        assert!(!suburb.name.is_empty());
        assert!(!suburb.postcode.is_empty());
        assert!(!suburb.state.is_empty());
        // Verify it's in the list
        assert!(AUSTRALIAN_SUBURBS.iter().any(|s| s.name == suburb.name));
    }

    #[test]
    fn generate_au_address_has_au_country() {
        let mut rng = rand::rng();
        let addr = generate_au_address(&mut rng);
        assert_eq!(addr["country"], "AU");
        assert!(addr["state"].as_str().unwrap().len() == 2
            || addr["state"].as_str().unwrap().len() == 3);
        assert!(!addr["city"].as_str().unwrap().is_empty());
    }

    #[test]
    fn apply_hcpd_bulk_fixes_organization() {
        let mut rng = rand::rng();
        let mut resource = serde_json::json!({
            "resourceType": "Organization",
            "id": "organization-1"
        });
        let mut reg_map = HashMap::new();
        let vs_systems = HashMap::new();
        let cs_codes = HashMap::new();

        apply_hcpd_bulk_fixes(
            &mut resource,
            "Organization",
            "organization-1",
            &mut reg_map,
            &vs_systems,
            &cs_codes,
            &mut rng,
        );

        // Should have ABN and HPI-O identifiers
        let identifiers = resource["identifier"].as_array().unwrap();
        assert_eq!(identifiers.len(), 2);
        assert_eq!(identifiers[0]["system"], "http://hl7.org.au/id/abn");
        assert_eq!(
            identifiers[1]["system"],
            "http://ns.electronichealth.net.au/id/hi/hpio/1.0"
        );

        // Should have address with AU defaults
        assert_eq!(resource["address"][0]["country"], "AU");
        assert_eq!(resource["address"][0]["state"], "NSW");
    }

    #[test]
    fn apply_hcpd_bulk_fixes_practitioner() {
        let mut rng = rand::rng();
        let mut resource = serde_json::json!({
            "resourceType": "Practitioner",
            "id": "practitioner-1"
        });
        let mut reg_map = HashMap::new();
        let vs_systems = HashMap::new();
        let cs_codes = HashMap::new();

        apply_hcpd_bulk_fixes(
            &mut resource,
            "Practitioner",
            "practitioner-1",
            &mut reg_map,
            &vs_systems,
            &cs_codes,
            &mut rng,
        );

        // Should have HPI-I identifier
        let identifiers = resource["identifier"].as_array().unwrap();
        assert_eq!(identifiers.len(), 1);
        assert_eq!(
            identifiers[0]["system"],
            "http://ns.electronichealth.net.au/id/hi/hpii/1.0"
        );

        // Should have qualification with AHPRA registration
        let quals = resource["qualification"].as_array().unwrap();
        assert_eq!(quals.len(), 1);
        let reg_id = &quals[0]["identifier"][0];
        assert_eq!(
            reg_id["system"],
            "http://hl7.org.au/id/ahpra-registration-number"
        );
        assert!(reg_id["value"].as_str().unwrap().starts_with("MED"));

        // Registration should be tracked
        assert!(reg_map.contains_key("practitioner-1"));
    }

    #[test]
    fn apply_hcpd_bulk_fixes_practitioner_role() {
        let mut rng = rand::rng();
        let mut resource = serde_json::json!({
            "resourceType": "PractitionerRole",
            "id": "practitionerrole-1",
            "practitioner": {
                "reference": "Practitioner/practitioner-1"
            }
        });
        let mut reg_map = HashMap::new();
        reg_map.insert(
            "practitioner-1".to_string(),
            "MED1234567890".to_string(),
        );
        let vs_systems = HashMap::new();
        let cs_codes = HashMap::new();

        apply_hcpd_bulk_fixes(
            &mut resource,
            "PractitionerRole",
            "practitionerrole-1",
            &mut reg_map,
            &vs_systems,
            &cs_codes,
            &mut rng,
        );

        // Should have local identifier and AHPRA registration
        let identifiers = resource["identifier"].as_array().unwrap();
        assert_eq!(identifiers.len(), 2);
        assert_eq!(
            identifiers[0]["system"],
            "http://digitalhealth.gov.au/fhir/hcpd/id/hcpd-local-identifier"
        );
        assert_eq!(
            identifiers[1]["system"],
            "http://hl7.org.au/id/ahpra-registration-number"
        );
        // Should pick up the registration from the practitioner
        assert_eq!(
            identifiers[1]["value"],
            "MED1234567890"
        );
    }

    #[test]
    fn apply_hcpd_bulk_fixes_location() {
        let mut rng = rand::rng();
        let mut resource = serde_json::json!({
            "resourceType": "Location",
            "id": "location-1"
        });
        let mut reg_map = HashMap::new();
        let vs_systems = HashMap::new();
        let cs_codes = HashMap::new();

        apply_hcpd_bulk_fixes(
            &mut resource,
            "Location",
            "location-1",
            &mut reg_map,
            &vs_systems,
            &cs_codes,
            &mut rng,
        );

        assert_eq!(resource["type"][0]["text"], "Healthcare service location");
    }

    #[test]
    fn apply_hcpd_bulk_fixes_healthcare_service() {
        let mut rng = rand::rng();
        let mut resource = serde_json::json!({
            "resourceType": "HealthcareService",
            "id": "healthcareservice-1"
        });
        let mut reg_map = HashMap::new();
        let vs_systems = HashMap::new();
        let cs_codes = HashMap::new();

        apply_hcpd_bulk_fixes(
            &mut resource,
            "HealthcareService",
            "healthcareservice-1",
            &mut reg_map,
            &vs_systems,
            &cs_codes,
            &mut rng,
        );

        // Should have SNOMED coding for type
        let types = resource["type"].as_array().unwrap();
        assert_eq!(types[0]["coding"][0]["code"], "408443003");
        assert_eq!(
            types[0]["coding"][0]["display"],
            "General medical practice"
        );
    }
}
