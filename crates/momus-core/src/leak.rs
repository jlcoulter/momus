//! Shared information-leak pattern detection.
//!
//! Consolidates leak detection logic used by both `momus-fuzz` and `momus-guard`
//! so that both crates detect the same patterns from a single source of truth.

/// A single information leak match found in a response body.
#[derive(Debug, Clone, PartialEq)]
pub struct InfoLeakMatch {
    /// The matched pattern text.
    pub pattern: &'static str,
    /// Category of the leak (e.g. "leak").
    pub category: &'static str,
    /// Human-readable description of what was found.
    pub description: &'static str,
}

/// A canonical leak-pattern entry.
#[derive(Debug, Clone)]
struct LeakPattern {
    /// Lowercase substring to search for.
    pattern: &'static str,
    /// Category label.
    category: &'static str,
    /// Human-readable description.
    description: &'static str,
}

/// Canonical list of information-leak patterns with categories and descriptions.
///
/// Every pattern is lowercased; detection is case-insensitive.
const LEAK_PATTERNS: &[LeakPattern] = &[
    // ── Stack traces & exceptions ──
    LeakPattern {
        pattern: "stack trace",
        category: "leak",
        description: "Response contains a stack trace, potentially revealing internal code paths",
    },
    LeakPattern {
        pattern: "exception",
        category: "leak",
        description: "Response contains exception details",
    },
    LeakPattern {
        pattern: "syntaxerror",
        category: "leak",
        description: "Response contains a syntax error message",
    },
    LeakPattern {
        pattern: "referenceerror",
        category: "leak",
        description: "Response contains a reference error",
    },
    LeakPattern {
        pattern: "typeerror",
        category: "leak",
        description: "Response contains a type error",
    },
    LeakPattern {
        pattern: "traceback",
        category: "leak",
        description: "Response contains a Python traceback",
    },
    // ── SQL / database errors ──
    LeakPattern {
        pattern: "sql syntax",
        category: "leak",
        description: "Response contains SQL syntax information, potential SQL injection vector",
    },
    LeakPattern {
        pattern: "mysql_error",
        category: "leak",
        description: "Response contains a MySQL error message",
    },
    LeakPattern {
        pattern: "ora-",
        category: "leak",
        description: "Response contains an Oracle database error",
    },
    LeakPattern {
        pattern: "postgresql",
        category: "leak",
        description: "Response references PostgreSQL, potential database error leak",
    },
    LeakPattern {
        pattern: "select * from",
        category: "leak",
        description: "Response contains SQL query, potential SQL injection",
    },
    LeakPattern {
        pattern: "insert into",
        category: "leak",
        description: "Response contains SQL query, potential SQL injection",
    },
    LeakPattern {
        pattern: "drop table",
        category: "leak",
        description: "Response contains SQL DROP statement, potential SQL injection",
    },
    LeakPattern {
        pattern: "union select",
        category: "leak",
        description: "Response contains SQL UNION query, potential SQL injection",
    },
    // ── Generic error messages ──
    LeakPattern {
        pattern: "fatal:",
        category: "leak",
        description: "Response contains a fatal error prefix",
    },
    LeakPattern {
        pattern: "warning:",
        category: "leak",
        description: "Response contains a warning message",
    },
    LeakPattern {
        pattern: "fatal error",
        category: "leak",
        description: "Response contains a fatal error message",
    },
    LeakPattern {
        pattern: "internal error",
        category: "leak",
        description: "Response contains an internal error message",
    },
    LeakPattern {
        pattern: "internal server error",
        category: "leak",
        description: "Generic internal server error (may be acceptable)",
    },
    LeakPattern {
        pattern: "debug",
        category: "leak",
        description: "Response contains debug information",
    },
    // ── Language / framework internals ──
    LeakPattern {
        pattern: "com.",
        category: "leak",
        description: "Response contains a Java package name, potentially revealing code structure",
    },
    LeakPattern {
        pattern: "org.",
        category: "leak",
        description: "Response contains a Java/org package name, potentially revealing code structure",
    },
    LeakPattern {
        pattern: "java.lang",
        category: "leak",
        description: "Response contains a Java language class reference",
    },
    LeakPattern {
        pattern: "system.",
        category: "leak",
        description: "Response contains a .NET System namespace reference",
    },
    LeakPattern {
        pattern: "system.io",
        category: "leak",
        description: "Response contains a .NET System.IO namespace reference",
    },
    LeakPattern {
        pattern: "microsoft",
        category: "leak",
        description: "Response references Microsoft framework internals",
    },
    // ── System file paths & credentials ──
    LeakPattern {
        pattern: "root:x:",
        category: "leak",
        description: "Response contains password file data (/etc/passwd)",
    },
    LeakPattern {
        pattern: "daemon:x:",
        category: "leak",
        description: "Response contains password file data (daemon entry)",
    },
    LeakPattern {
        pattern: "/etc/passwd",
        category: "leak",
        description: "Response references /etc/passwd, potential path traversal",
    },
    LeakPattern {
        pattern: "/etc/shadow",
        category: "leak",
        description: "Response references /etc/shadow, potential path traversal",
    },
];

/// Detect information-leak patterns in a response body.
///
/// Returns a list of [`InfoLeakMatch`] values for every distinct pattern found.
/// The search is case-insensitive.
///
/// # Example
///
/// ```
/// use momus_core::leak::detect_info_leaks;
///
/// let matches = detect_info_leaks("stack trace: at main()");
/// assert_eq!(matches.len(), 1);
/// assert_eq!(matches[0].pattern, "stack trace");
///
/// let matches = detect_info_leaks(r#"{"status": "ok"}"#);
/// assert!(matches.is_empty());
/// ```
pub fn detect_info_leaks(body: &str) -> Vec<InfoLeakMatch> {
    let lower = body.to_lowercase();
    let mut results = Vec::new();

    for entry in LEAK_PATTERNS {
        if lower.contains(entry.pattern) {
            results.push(InfoLeakMatch {
                pattern: entry.pattern,
                category: entry.category,
                description: entry.description,
            });
        }
    }

    results
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_detect_stack_trace() {
        let matches = detect_info_leaks("stack trace: at main() in file.php line 42");
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.pattern == "stack trace"));
    }

    #[test]
    fn test_detect_exception() {
        let matches = detect_info_leaks("Fatal error: syntax error");
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.pattern == "fatal error"));
    }

    #[test]
    fn test_detect_sql() {
        let matches = detect_info_leaks("SELECT * FROM users");
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.pattern == "select * from"));
    }

    #[test]
    fn test_detect_sql_syntax() {
        let matches = detect_info_leaks("SQL syntax near 'SELECT'");
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.pattern == "sql syntax"));
    }

    #[test]
    fn test_detect_ora() {
        let matches = detect_info_leaks("ORA-00942: table not found");
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.pattern == "ora-"));
    }

    #[test]
    fn test_detect_path() {
        let matches = detect_info_leaks("/etc/passwd");
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.pattern == "/etc/passwd"));
    }

    #[test]
    fn test_detect_passwd_entry() {
        let matches = detect_info_leaks("root:x:0:0:root:/root:/bin/bash");
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.pattern == "root:x:"));
    }

    #[test]
    fn test_no_false_positive() {
        let matches = detect_info_leaks(r#"{"status": "ok"}"#);
        assert!(matches.is_empty());
    }

    #[test]
    fn test_no_match_clean_text() {
        let matches = detect_info_leaks("hello world");
        assert!(matches.is_empty());
    }

    #[test]
    fn test_multiple_matches() {
        let matches = detect_info_leaks("stack trace: SELECT * FROM users");
        assert!(matches.len() >= 2);
        assert!(matches.iter().any(|m| m.pattern == "stack trace"));
        assert!(matches.iter().any(|m| m.pattern == "select * from"));
    }

    #[test]
    fn test_case_insensitive() {
        let matches = detect_info_leaks("STACK TRACE: At Main()");
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.pattern == "stack trace"));
    }

    #[test]
    fn test_detect_insert_into() {
        let matches = detect_info_leaks("INSERT INTO users VALUES (1)");
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.pattern == "insert into"));
    }

    #[test]
    fn test_detect_drop_table() {
        let matches = detect_info_leaks("DROP TABLE users");
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.pattern == "drop table"));
    }

    #[test]
    fn test_detect_union_select() {
        let matches = detect_info_leaks("UNION SELECT * FROM admin");
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.pattern == "union select"));
    }

    #[test]
    fn test_detect_traceback() {
        let matches = detect_info_leaks("Traceback (most recent call last):");
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.pattern == "traceback"));
    }

    #[test]
    fn test_detect_internal_server_error() {
        let matches = detect_info_leaks("Internal Server Error");
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.pattern == "internal server error"));
    }

    #[test]
    fn test_detect_java_lang() {
        let matches = detect_info_leaks("java.lang.NullPointerException");
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.pattern == "java.lang"));
    }

    #[test]
    fn test_detect_etc_shadow() {
        let matches = detect_info_leaks("/etc/shadow");
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.pattern == "/etc/shadow"));
    }
}
