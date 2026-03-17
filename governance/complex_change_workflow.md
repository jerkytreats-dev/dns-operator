# Complex Change Workflow Governance

Date: 2026-03-16
Status: active
Scope: user-triggered workflow for complex change requests

## Purpose

Define one optional workflow for complex work that benefits from phased execution, dependency tracking, and explicit verification gates.

This workflow is opt in and not the default for routine change requests.

## Activation

Workflow activation requires explicit user intent.

Activation paths:

- user requests complex workflow mode
- agent recommends complex workflow mode and user confirms

Without explicit user confirmation, work remains on the default workflow path.

## CI Enforcement

CI ignores this workflow.

## Required Artifacts When Active

When this workflow is active, create and maintain the relevant plan material under `docs/plan/`.

Required structure:

- overview with objective and outcome
- development phases with dependency order
- per-phase goal, tasks, exit criteria, and key seams
- verification strategy and gate definitions
- implementation order summary
- related documentation links
- short exception list for non-default command or rollout behavior

## Branch And Commit Model When Active

- one feature branch for the full scoped plan by default
- optional short-lived working branches for parallel execution
- each major phase should land as an atomic commit by default
- tiny coupled doc updates may share a commit when splitting adds noise

## Evidence And Completion Tracking

- update plan task completion as work lands
- capture verification evidence for each active gate
- record unresolved risks when present

## Deactivation

This workflow deactivates when either condition is true:

- the user requests return to the default workflow
- the scoped complex work is complete and closed out in `docs/plan/`
