export default {
    name: 'logsComponent',
    setup() {
        return {
            logInterval: null,

            async fetchLogs() {
                const gStore = Alpine.store('global');
                if (gStore.currentTab !== 'logs') return;
                try {
                    const res = await fetch('/api/admin/logs');
                    const rawText = await res.text();
                    let lines = rawText.split('\\n');
                    if (gStore.logLevelFilter !== 'all') {
                        const levelStr = `level=${gStore.logLevelFilter.toUpperCase()}`;
                        lines = lines.filter(line => line.includes(levelStr) || line.trim() === '');
                    }
                    gStore.logsText = lines.join('\\n');
                    
                    if (gStore.isAutoScroll) {
                        setTimeout(() => {
                            const container = document.getElementById('logContainer');
                            if (container) container.scrollTop = container.scrollHeight;
                        }, 50);
                    }
                } catch (e) {
                    console.error("Fetch logs failed", e);
                }
            },

            async fetchDebug() {
                try {
                    const res = await fetch('/api/admin/debug');
                    const json = await res.json();
                    Alpine.store('global').debugEnabled = json.debug;
                } catch (e) { console.error(e) }
            },

            async toggleDebug() {
                const gStore = Alpine.store('global');
                try {
                    const newVal = !gStore.debugEnabled;
                    const res = await fetch('/api/admin/debug', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ enabled: newVal })
                    });
                    const json = await res.json();
                    gStore.debugEnabled = json.debug;
                    gStore.showToast(gStore.debugEnabled ? gStore.t('debug_enabled') : gStore.t('debug_disabled'));
                } catch(e) {
                    gStore.showToast(gStore.t("debug_switch_failed"), 'error');
                }
            },

            init() {
                this.fetchDebug();
                this.fetchLogs();
                this.logInterval = setInterval(() => {
                    this.fetchLogs();
                }, 3000);
            },

            destroy() {
                if (this.logInterval) clearInterval(this.logInterval);
            }
        };
    },
    template: `
        <div x-show="$store.global.currentTab === 'logs'" class="max-w-6xl mx-auto flex flex-col h-full w-full relative">
            <div class="flex justify-between items-center mb-6 shrink-0">
                <h2 class="text-3xl font-bold" x-text="$store.global.t('tab_logs_title')"></h2>
                <div class="flex gap-3">
                    <select name="store_global_logLevelFilter" x-model="$store.global.logLevelFilter" @change="fetchLogs" class="select select-bordered select-sm">
                        <option value="all" x-text="$store.global.t('log_level_all_desc')"></option>
                        <option value="info">INFO</option>
                        <option value="warn">WARN</option>
                        <option value="error">ERROR</option>
                        <option value="debug">DEBUG</option>
                    </select>
                    <button @click="toggleDebug" class="btn btn-sm" :class="$store.global.debugEnabled ? 'btn-secondary shadow-lg shadow-secondary/20' : 'btn-outline'">
                        <span>🐛</span>
                        <span x-text="$store.global.debugEnabled ? 'DEBUG ON' : 'DEBUG OFF'"></span>
                    </button>
                    <button @click="$store.global.isAutoScroll = !$store.global.isAutoScroll" class="btn btn-sm" :class="$store.global.isAutoScroll ? 'btn-success shadow-lg shadow-success/20' : 'btn-outline'">
                        <template x-if="$store.global.isAutoScroll">
                            <span class="relative flex h-2 w-2">
                                <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-success opacity-75"></span>
                                <span class="relative inline-flex rounded-full h-2 w-2 bg-success"></span>
                            </span>
                        </template>
                        <span x-text="$store.global.t('auto_scroll_text') + ($store.global.isAutoScroll ? ' ON' : ' OFF')"></span>
                    </button>
                    <button @click="fetchLogs" class="btn btn-sm btn-outline" x-text="$store.global.t('btn_refresh_logs')"></button>
                </div>
            </div>
            
            <div class="card bg-base-300 shadow-inner flex-1 overflow-hidden flex flex-col border border-base-content/10">
                <div class="bg-base-200 border-b border-base-content/10 px-4 py-2 flex items-center gap-2 shrink-0">
                    <div class="flex gap-1.5">
                        <div class="w-3 h-3 rounded-full bg-error"></div>
                        <div class="w-3 h-3 rounded-full bg-warning"></div>
                        <div class="w-3 h-3 rounded-full bg-success"></div>
                    </div>
                    <span class="text-base-content/50 text-xs font-mono ml-2">hermes.log</span>
                </div>
                <div class="flex-1 overflow-auto p-4 font-mono text-[13px] leading-relaxed" id="logContainer">
                    <pre class="whitespace-pre-wrap break-all" x-text="$store.global.logsText"></pre>
                </div>
            </div>
        </div>
    `
};
