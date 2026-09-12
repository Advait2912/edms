// =========================================
// EDMS PENDING CHANGES
// Reusable compute-result UI
// =========================================

(function () {
    "use strict";

    const state = {
        pending: []
    };

    function normalizeOperations(payload) {
        if (!payload) {
            return [];
        }

        const candidates = [
            payload.operations,
            payload.changes,
            payload.ops,
            payload.items,
            payload.results
        ];

        for (const value of candidates) {
            if (Array.isArray(value)) {
                return value;
            }
        }

        if (payload.operation) {
            return [payload.operation];
        }

        if (payload.change) {
            return [payload.change];
        }

        return [];
    }

    function normalizeChange(change, payload = {}) {
        if (typeof change === "string") {
            return {
                id: `change-${Date.now()}-${Math.random()}`,
                title: change,
                description: "",
                endpointId:
                    payload.endpoint_id ??
                    payload.endpointId ??
                    null,
                raw: change
            };
        }

        if (!change || typeof change !== "object") {
            return null;
        }

        return {
            id:
                change.id ??
                change.operation_id ??
                change.operationId ??
                `change-${Date.now()}-${Math.random()}`,

            title:
                change.name ??
                change.operation ??
                change.type ??
                change.action ??
                "Computed change",

            description:
                change.description ??
                change.message ??
                "",

            endpointId:
                change.endpoint_id ??
                change.endpointId ??
                payload.endpoint_id ??
                payload.endpointId ??
                null,

            raw: change
        };
    }

    function getOrCreateUI() {
        let container =
            document.getElementById(
                "edmsPendingChanges"
            );

        if (container) {
            return container;
        }

        container =
            document.createElement("div");

        container.id =
            "edmsPendingChanges";

        container.className = `
            fixed
            bottom-5
            right-5
            z-[9998]
            hidden
        `;

        container.innerHTML = `
            <div
                class="
                    w-[340px]
                    max-w-[calc(100vw-32px)]
                    rounded-xl
                    border
                    border-slate-700
                    bg-slate-900
                    shadow-2xl
                    overflow-hidden
                "
            >
                <div
                    class="
                        flex
                        items-center
                        justify-between
                        px-4
                        py-3
                        border-b
                        border-slate-700
                    "
                >
                    <div>
                        <p
                            class="
                                text-sm
                                font-semibold
                                text-slate-100
                            "
                        >
                            Changes are ready
                        </p>

                        <p
                            id="edmsPendingChangesCount"
                            class="
                                mt-0.5
                                text-xs
                                text-slate-400
                            "
                        >
                            0 pending
                        </p>
                    </div>

                    <button
                        type="button"
                        id="edmsPendingChangesClose"
                        class="
                            text-slate-400
                            hover:text-white
                        "
                        aria-label="Close"
                    >
                        ×
                    </button>
                </div>

                <div
                    id="edmsPendingChangesList"
                    class="
                        max-h-56
                        overflow-y-auto
                        px-4
                        py-3
                        space-y-2
                    "
                ></div>

                <div
                    class="
                        flex
                        justify-end
                        gap-2
                        px-4
                        py-3
                        border-t
                        border-slate-700
                    "
                >
                    <button
                        type="button"
                        id="edmsPendingChangesLater"
                        class="
                            rounded-md
                            border
                            border-slate-700
                            px-3
                            py-2
                            text-xs
                            text-slate-300
                            hover:bg-slate-800
                        "
                    >
                        Later
                    </button>

                    <button
                        type="button"
                        id="edmsPendingChangesApply"
                        class="
                            rounded-md
                            bg-cyan-500
                            px-3
                            py-2
                            text-xs
                            font-semibold
                            text-slate-950
                            hover:bg-cyan-400
                        "
                    >
                        Apply Changes
                    </button>
                </div>
            </div>
        `;

        document.body.appendChild(container);

        container
            .querySelector(
                "#edmsPendingChangesClose"
            )
            .addEventListener(
                "click",
                hide
            );

        container
            .querySelector(
                "#edmsPendingChangesLater"
            )
            .addEventListener(
                "click",
                hide
            );

        container
            .querySelector(
                "#edmsPendingChangesApply"
            )
            .addEventListener(
                "click",
                apply
            );

        return container;
    }

    function render() {
        const container =
            getOrCreateUI();

        const list =
            container.querySelector(
                "#edmsPendingChangesList"
            );

        const count =
            container.querySelector(
                "#edmsPendingChangesCount"
            );

        if (!list || !count) {
            return;
        }

        count.textContent =
            `${state.pending.length} pending`;

        list.innerHTML = "";

        state.pending.forEach(
            change => {

                const item =
                    document.createElement("div");

                item.className = `
                    rounded-lg
                    border
                    border-slate-800
                    bg-slate-950/50
                    px-3
                    py-2
                `;

                item.innerHTML = `
                    <p
                        class="
                            text-xs
                            font-medium
                            text-slate-200
                        "
                    >
                        ${escapeHTML(change.title)}
                    </p>

                    ${
                        change.description
                            ? `
                                <p
                                    class="
                                        mt-1
                                        text-[11px]
                                        text-slate-500
                                    "
                                >
                                    ${escapeHTML(
                                        change.description
                                    )}
                                </p>
                            `
                            : ""
                    }
                `;

                list.appendChild(item);
            }
        );
    }

    function show() {
        const container =
            getOrCreateUI();

        container.classList.remove(
            "hidden"
        );

        render();
    }

    function hide() {
        const container =
            document.getElementById(
                "edmsPendingChanges"
            );

        if (!container) {
            return;
        }

        container.classList.add(
            "hidden"
        );
    }

    function add(payload) {
        const operations =
            normalizeOperations(payload);

        if (operations.length === 0) {
            return false;
        }

        const normalized =
            operations
                .map(
                    operation =>
                        normalizeChange(
                            operation,
                            payload
                        )
                )
                .filter(Boolean);

        if (normalized.length === 0) {
            return false;
        }

        normalized.forEach(
            change => {

                const exists =
                    state.pending.some(
                        existing =>
                            String(
                                existing.id
                            ) ===
                            String(
                                change.id
                            )
                    );

                if (!exists) {
                    state.pending.push(
                        change
                    );
                }

            }
        );

        show();

        return true;
    }

    function apply() {
        const changes =
            [...state.pending];

        if (changes.length === 0) {
            hide();
            return;
        }

        document.dispatchEvent(
            new CustomEvent(
                "edms:apply-pending-changes",
                {
                    detail: {
                        changes
                    }
                }
            )
        );

        state.pending = [];

        hide();
    }

    function clear() {
        state.pending = [];
        hide();
    }

    function getPending() {
        return [...state.pending];
    }

    function escapeHTML(value) {
        return String(
            value ?? ""
        )
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;")
            .replace(/'/g, "&#039;");
    }

    window.EdmsPendingChanges = {
        add,
        show,
        hide,
        clear,
        getPending
    };
})();