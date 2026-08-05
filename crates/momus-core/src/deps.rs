/// Generic dependency resolver using topological sort.
///
/// Given a map of items and their dependencies, produces an ordering
/// where dependencies come before dependents. Handles circular dependencies
/// gracefully by collapsing them into strongly connected components (SCCs).
use std::collections::{HashMap, HashSet};

/// A dependency entry: (item, [dependencies]).
///
/// Each entry declares that `item` depends on all items in the `dependencies` list.
/// Dependencies must be created/setup before the item.
pub type DependencyMap = Vec<(String, Vec<String>)>;

/// Resolve a creation/setup order from a dependency map using topological sort.
///
/// Returns items in dependency-first order: if A depends on B, B comes before A.
/// Circular dependencies are handled by collapsing SCCs — items in a cycle are
/// grouped together (order within the cycle is arbitrary).
///
/// # Errors
///
/// Returns an error only if the graph cannot be constructed (should not happen
/// with valid input).
pub fn resolve_order(deps: &DependencyMap) -> anyhow::Result<Vec<String>> {
    let mut graph = petgraph::graph::DiGraph::<String, ()>::new();
    let mut node_indices: HashMap<String, petgraph::graph::NodeIndex> = HashMap::new();

    // Add nodes for all items
    for (item, _) in deps {
        let idx = graph.add_node(item.clone());
        node_indices.insert(item.clone(), idx);
    }

    // Add edges: if A depends on B, then B must come before A
    // Edge direction: A → B means "A depends on B"
    for (item, references) in deps {
        let from_idx = node_indices.get(item).unwrap();
        for dep in references {
            if let Some(to_idx) = node_indices.get(dep) {
                graph.add_edge(*from_idx, *to_idx, ());
            }
        }
    }

    // Find strongly connected components (SCCs) to handle cycles.
    // kosaraju_scc returns SCCs in reverse topological order,
    // so we can simply iterate to get dependencies-first ordering.
    let sccs = petgraph::algo::kosaraju_scc(&graph);

    // Build SCC DAG for topological sort
    let mut scc_id: HashMap<petgraph::graph::NodeIndex, usize> = HashMap::new();
    for (i, scc) in sccs.iter().enumerate() {
        for &node in scc {
            scc_id.insert(node, i);
        }
    }

    // Build adjacency list for SCC DAG
    let mut scc_deps: Vec<HashSet<usize>> = vec![HashSet::new(); sccs.len()];
    for edge in graph.raw_edges() {
        let from_scc = scc_id[&edge.source()];
        let to_scc = scc_id[&edge.target()];
        if from_scc != to_scc {
            scc_deps[from_scc].insert(to_scc);
        }
    }

    // Kahn's algorithm on SCC DAG: process SCCs with no remaining dependencies first.
    // Edge A → B means "A depends on B", so B should come first.
    // dep_count[i] = number of SCCs that i depends on (out-degree in SCC DAG).
    let mut dep_count: Vec<usize> = sccs
        .iter()
        .enumerate()
        .map(|(i, _)| scc_deps[i].len())
        .collect();
    let mut order = Vec::new();

    // Start with SCCs that have no dependencies
    let mut queue: Vec<usize> = (0..sccs.len()).filter(|&i| dep_count[i] == 0).collect();

    while let Some(scc_idx) = queue.pop() {
        // Emit all nodes in this SCC (within a cycle, order doesn't matter much)
        for &node in &sccs[scc_idx] {
            order.push(graph[node].clone());
        }
        // Remove edges: for any SCC that depended on this one, decrement its count
        for other in 0..sccs.len() {
            if scc_deps[other].contains(&scc_idx) {
                scc_deps[other].remove(&scc_idx);
                dep_count[other] -= 1;
                if dep_count[other] == 0 {
                    queue.push(other);
                }
            }
        }
    }

    // Any remaining SCCs (unreachable cycles) — just append them
    let ordered_set: HashSet<String> = order.iter().cloned().collect();
    for (item, _) in deps {
        if !ordered_set.contains(item) {
            order.push(item.clone());
        }
    }

    // Also add items that weren't in the dependency map but were referenced
    let all_referenced: HashSet<String> = deps
        .iter()
        .flat_map(|(_, refs)| refs.iter())
        .cloned()
        .collect();
    let existing: HashSet<String> = order.iter().cloned().collect();

    for item in all_referenced {
        if !existing.contains(&item) {
            order.push(item);
        }
    }

    Ok(order)
}

/// Merge auto-resolved order with user-specified overrides.
///
/// User overrides take precedence: items in the user list appear in that order.
/// Items not in the user list are appended in auto-resolved order.
pub fn merge_order(auto_order: &[String], user_order: &[String]) -> Vec<String> {
    if user_order.is_empty() {
        return auto_order.to_vec();
    }

    let mut result: Vec<String> = user_order.to_vec();
    let user_set: HashSet<String> = user_order.iter().cloned().collect();

    for item in auto_order {
        if !user_set.contains(item) && !result.contains(item) {
            result.push(item.clone());
        }
    }

    result
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn resolve_simple_dependency() {
        let deps = vec![
            (
                "Observation".to_string(),
                vec!["Patient".to_string(), "Encounter".to_string()],
            ),
            ("Encounter".to_string(), vec!["Patient".to_string()]),
            ("Patient".to_string(), vec![]),
        ];
        let order = resolve_order(&deps).expect("should resolve simple dependency order");

        let patient_idx = order
            .iter()
            .position(|r| r == "Patient")
            .expect("Patient should be in creation order");
        let encounter_idx = order
            .iter()
            .position(|r| r == "Encounter")
            .expect("Encounter should be in creation order");
        let observation_idx = order
            .iter()
            .position(|r| r == "Observation")
            .expect("Observation should be in creation order");

        assert!(
            patient_idx < encounter_idx,
            "Patient should come before Encounter"
        );
        assert!(
            patient_idx < observation_idx,
            "Patient should come before Observation"
        );
        assert!(
            encounter_idx < observation_idx,
            "Encounter should come before Observation"
        );
    }

    #[test]
    fn resolve_circular_dependency() {
        // Circular dependencies should be handled gracefully, not cause an error.
        let deps = vec![
            ("A".to_string(), vec!["B".to_string()]),
            ("B".to_string(), vec!["A".to_string()]),
        ];
        let order = resolve_order(&deps).expect("should resolve circular dependency order");
        assert_eq!(order.len(), 2);
        assert!(order.contains(&"A".to_string()));
        assert!(order.contains(&"B".to_string()));
    }

    #[test]
    fn resolve_no_dependencies() {
        let deps = vec![("A".to_string(), vec![]), ("B".to_string(), vec![])];
        let order = resolve_order(&deps).expect("should resolve order with no dependencies");
        assert!(order.contains(&"A".to_string()));
        assert!(order.contains(&"B".to_string()));
    }

    #[test]
    fn resolve_chain() {
        let deps = vec![
            ("A".to_string(), vec!["B".to_string()]),
            ("B".to_string(), vec!["C".to_string()]),
            ("C".to_string(), vec![]),
        ];
        let order = resolve_order(&deps).expect("should resolve chain");
        let c = order.iter().position(|r| r == "C").unwrap();
        let b = order.iter().position(|r| r == "B").unwrap();
        let a = order.iter().position(|r| r == "A").unwrap();
        assert!(c < b, "C before B");
        assert!(b < a, "B before A");
    }

    #[test]
    fn merge_order_with_override() {
        let auto = vec!["A".to_string(), "B".to_string(), "C".to_string()];
        let user = vec!["B".to_string(), "A".to_string()];
        let merged = merge_order(&auto, &user);

        assert_eq!(merged[0], "B");
        assert_eq!(merged[1], "A");
        assert_eq!(merged[2], "C");
    }

    #[test]
    fn merge_order_empty_override() {
        let auto = vec!["A".to_string(), "B".to_string()];
        let user: Vec<String> = vec![];
        let merged = merge_order(&auto, &user);
        assert_eq!(merged, auto);
    }

    #[test]
    fn resolve_diamond() {
        // A depends on B and C, both B and C depend on D
        let deps = vec![
            ("A".to_string(), vec!["B".to_string(), "C".to_string()]),
            ("B".to_string(), vec!["D".to_string()]),
            ("C".to_string(), vec!["D".to_string()]),
            ("D".to_string(), vec![]),
        ];
        let order = resolve_order(&deps).expect("should resolve diamond");
        let d = order.iter().position(|r| r == "D").unwrap();
        let b = order.iter().position(|r| r == "B").unwrap();
        let c = order.iter().position(|r| r == "C").unwrap();
        let a = order.iter().position(|r| r == "A").unwrap();
        assert!(d < b, "D before B");
        assert!(d < c, "D before C");
        assert!(b < a, "B before A");
        assert!(c < a, "C before A");
    }

    // -------------------------------------------------------------------------
    // Property-based tests with proptest
    // -------------------------------------------------------------------------

    /// Generate a random dependency graph and verify that the resolved order
    /// respects all declared dependencies (dependencies come before dependents).
    use proptest::prelude::*;

    proptest! {
        #[test]
        fn prop_resolve_order_invariant(
            deps in prop::collection::vec(
                ("[A-Z]", prop::collection::vec("[A-Z]", 0..4)),
                1..10,
            ),
        ) {
            // Deduplicate items with the same name by merging their dependencies
            let mut deduped: std::collections::HashMap<String, Vec<String>> =
                std::collections::HashMap::new();
            for (name, refs) in deps {
                let entry = deduped.entry(name.clone()).or_default();
                entry.extend(refs);
            }

            // Filter out self-references and references to non-existent items
            let item_set: std::collections::HashSet<String> = deduped.keys().cloned().collect();
            let clean_deps: Vec<(String, Vec<String>)> = deduped
                .into_iter()
                .map(|(name, refs)| {
                    let clean_refs: Vec<String> = refs
                        .into_iter()
                        .filter(|r| r != &name && item_set.contains(r))
                        .collect();
                    (name, clean_refs)
                })
                .collect();

            let order = resolve_order(&clean_deps)
                .expect("resolve_order should not fail for any valid input");

            // Verify all items are present in the output
            for (item, _) in &clean_deps {
                assert!(
                    order.contains(item),
                    "item {item} should be in the resolved order"
                );
            }

            // Verify dependency ordering: if A depends on B, B must come before A,
            // unless A and B are in a cycle (SCC), in which case order is arbitrary.
            // Build a graph to detect SCCs for cycle-aware checking.
            let mut graph = petgraph::graph::DiGraph::<String, ()>::new();
            let mut node_indices: std::collections::HashMap<String, _> =
                std::collections::HashMap::new();
            for (item, _) in &clean_deps {
                let idx = graph.add_node(item.clone());
                node_indices.insert(item.clone(), idx);
            }
            for (item, refs) in &clean_deps {
                let from_idx = node_indices[item];
                for dep in refs {
                    if let Some(&to_idx) = node_indices.get(dep) {
                        graph.add_edge(from_idx, to_idx, ());
                    }
                }
            }
            let sccs = petgraph::algo::kosaraju_scc(&graph);
            let mut scc_of: std::collections::HashMap<String, usize> =
                std::collections::HashMap::new();
            for (scc_id, scc) in sccs.iter().enumerate() {
                for node in scc {
                    scc_of.insert(graph[*node].clone(), scc_id);
                }
            }

            for (item, refs) in &clean_deps {
                let item_scc = scc_of[item];
                for dep in refs {
                    let dep_scc = scc_of[dep];
                    // Only check ordering if items are NOT in the same SCC (cycle)
                    if item_scc != dep_scc {
                        let item_pos = order.iter().position(|x| x == item).unwrap();
                        let dep_pos = order.iter().position(|x| x == dep).unwrap();
                        assert!(
                            dep_pos < item_pos,
                            "dependency {dep} should come before {item}"
                        );
                    }
                }
            }
        }
    }
}
