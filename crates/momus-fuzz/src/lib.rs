//! Payload mutation engine for Momus test plans.
//!
//! Takes a valid JSON payload and produces mutated variants to test
//! API robustness. The `Mutator` trait is the extension point.
//!
//! # Example
//!
//! ```rust,ignore
//! use momus_fuzz::{Mutator, mutators::BoundaryMutator};
//! use serde_json::json;
//!
//! let mutator = BoundaryMutator;
//! let base = json!({"name": "test", "count": 5});
//! let mutated = mutator.mutate(&base, 42);
//! assert_ne!(mutated, base);
//! ```

pub mod config;
pub mod mutators;
pub mod report;
pub mod runner;

pub use config::*;
pub use mutators::*;
pub use report::*;
pub use runner::*;

use serde_json::Value;

/// A mutator transforms a valid JSON payload into a mutated variant.
///
/// Implementations should be deterministic given the same seed —
/// the same `(base, seed)` pair should always produce the same mutation.
pub trait Mutator: Send + Sync {
    /// Human-readable name for this mutator.
    fn name(&self) -> &'static str;

    /// Produce a mutated variant of `base`.
    ///
    /// The `seed` parameter allows deterministic replay of specific mutations.
    fn mutate(&self, base: &Value, seed: u64) -> Value;
}
