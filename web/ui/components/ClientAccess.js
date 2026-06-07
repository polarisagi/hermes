export default {
    name: 'clientAccessComponent',
    setup() {
        return {
            statuses: [],
            loading: false,
            listenAddr: '127.0.0.1:27777',
            applyingClient: null,
            restoringClient: null,

            async fetchStatuses() {
                if (Alpine.store('global').currentTab !== 'client_access') return;
                this.loading = true;
                try {
                    const [statusRes, settingsRes] = await Promise.all([
                        fetch('/api/admin/client_access/status'),
                        fetch('/api/admin/settings')
                    ]);
                    if (statusRes.ok) {
                        const data = await statusRes.json();
                        this.statuses = data.clients || [];
                    }
                    if (settingsRes.ok) {
                        const settings = await settingsRes.json();
                        this.listenAddr = settings.listen_addr || '127.0.0.1:27777';
                    }
                } catch (e) {
                    console.error(e);
                } finally {
                    this.loading = false;
                }
            },

            async applyConfig(clientName) {
                const gStore = Alpine.store('global');
                const client = this.statuses.find(c => c.name === clientName);
                const displayName = client ? gStore.t('client_name_' + client.name) : clientName;
                if (!confirm(
                    gStore.lang === 'zh'
                        ? `确定要为 ${displayName} 注入代理配置吗？\n系统会自动备份原配置，方便随时恢复。`
                        : `Apply proxy config for ${displayName}?\nOriginal config will be backed up automatically.`
                )) return;

                this.applyingClient = clientName;
                try {
                    const res = await fetch('/api/admin/client_access/apply', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ client: clientName })
                    });
                    if (res.ok) {
                        gStore.showToast(gStore.t('client_apply_success'), 'success');
                        await this.fetchStatuses();
                    } else {
                        const errText = await res.text();
                        gStore.showToast(gStore.t('client_apply_failed') + ': ' + errText, 'error');
                    }
                } catch (e) {
                    gStore.showToast(gStore.t('network_error'), 'error');
                } finally {
                    this.applyingClient = null;
                }
            },

            async restoreConfig(clientName) {
                const gStore = Alpine.store('global');
                const client = this.statuses.find(c => c.name === clientName);
                const displayName = client ? gStore.t('client_name_' + client.name) : clientName;
                if (!confirm(
                    gStore.lang === 'zh'
                        ? `确定要恢复 ${displayName} 的原始配置吗？\n代理注入设置将被清除。`
                        : `Restore original config for ${displayName}?\nProxy injection settings will be removed.`
                )) return;

                this.restoringClient = clientName;
                try {
                    const res = await fetch('/api/admin/client_access/restore', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ client: clientName })
                    });
                    if (res.ok) {
                        gStore.showToast(gStore.t('client_restore_success'), 'success');
                        await this.fetchStatuses();
                    } else {
                        const errText = await res.text();
                        gStore.showToast(gStore.t('client_restore_failed') + ': ' + errText, 'error');
                    }
                } catch (e) {
                    gStore.showToast(gStore.t('network_error'), 'error');
                } finally {
                    this.restoringClient = null;
                }
            },

            getStatusBadge(client) {
                if (client.is_configured) return { label: Alpine.store('global').t('client_configured'), cls: 'badge-success' };
                if (client.is_installed) return { label: Alpine.store('global').t('client_not_configured'), cls: 'badge-warning' };
                return { label: Alpine.store('global').t('client_not_installed'), cls: 'badge-ghost' };
            },

            init() {
                this.fetchStatuses();
                this.$watch('$store.global.currentTab', (newTab) => {
                    if (newTab === 'client_access') this.fetchStatuses();
                });
            }
        };
    },
    template: `
        <div x-show="$store.global.currentTab === 'client_access'" class="max-w-5xl mx-auto w-full space-y-6">

            <!-- Header -->
            <div class="flex justify-between items-center">
                <div>
                    <h2 class="text-3xl font-bold" x-text="$store.global.t('tab_client_access_title')"></h2>
                    <p class="text-sm text-base-content/80 mt-1" x-text="$store.global.t('clients_setup_desc')"></p>
                </div>
                <button @click="fetchStatuses().then(() => $store.global.showToast($store.global.t('refresh_success'), 'success'))" class="btn btn-outline btn-sm shadow-sm gap-2">
                    <span :class="{'animate-spin': loading}">🔄</span>
                    <span x-text="$store.global.t('btn_refresh')"></span>
                </button>
            </div>

            <!-- Proxy address info banner -->
            <div class="alert shadow-sm border border-base-300 bg-base-200/50 block">
                <div class="flex items-center gap-2 mb-2">
                    <svg class="w-5 h-5 text-info flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M12 2a10 10 0 100 20A10 10 0 0012 2z"/>
                    </svg>
                    <p class="text-sm font-medium" x-text="$store.global.t('client_base_url_hint')"></p>
                </div>
                
                <div class="grid grid-cols-1 md:grid-cols-3 gap-4 mt-2">
                    <!-- OpenAI -->
                    <div class="bg-base-100 p-3 rounded border border-base-300">
                        <div class="text-xs font-bold mb-1 opacity-70" x-text="$store.global.t('client_protocol_openai')"></div>
                        <div class="flex items-center gap-2">
                            <code class="text-xs bg-base-200 px-2 py-0.5 rounded font-mono flex-1 truncate" x-text="'http://' + listenAddr + '/v1/openai'"></code>
                            <button @click="navigator.clipboard.writeText('http://'+listenAddr+'/v1/openai').then(()=>$store.global.showToast($store.global.lang==='zh'?'已复制':'Copied!'))" class="btn btn-ghost btn-xs px-1">📋</button>
                        </div>
                    </div>
                    <!-- Anthropic -->
                    <div class="bg-base-100 p-3 rounded border border-base-300">
                        <div class="text-xs font-bold mb-1 opacity-70" x-text="$store.global.t('client_protocol_anthropic')"></div>
                        <div class="flex items-center gap-2">
                            <code class="text-xs bg-base-200 px-2 py-0.5 rounded font-mono flex-1 truncate" x-text="'http://' + listenAddr + '/v1/anthropic'"></code>
                            <button @click="navigator.clipboard.writeText('http://'+listenAddr+'/v1/anthropic').then(()=>$store.global.showToast($store.global.lang==='zh'?'已复制':'Copied!'))" class="btn btn-ghost btn-xs px-1">📋</button>
                        </div>
                    </div>
                    <!-- Google -->
                    <div class="bg-base-100 p-3 rounded border border-base-300">
                        <div class="text-xs font-bold mb-1 opacity-70" x-text="$store.global.t('client_protocol_google')"></div>
                        <div class="flex items-center gap-2">
                            <code class="text-xs bg-base-200 px-2 py-0.5 rounded font-mono flex-1 truncate" x-text="'http://' + listenAddr + '/v1/google'"></code>
                            <button @click="navigator.clipboard.writeText('http://'+listenAddr+'/v1/google').then(()=>$store.global.showToast($store.global.lang==='zh'?'已复制':'Copied!'))" class="btn btn-ghost btn-xs px-1">📋</button>
                        </div>
                    </div>
                </div>
            </div>

            <!-- Loading skeleton -->
            <template x-if="loading && statuses.length === 0">
                <div class="grid gap-4">
                    <template x-for="i in 4">
                        <div class="card bg-base-100 shadow animate-pulse">
                            <div class="card-body py-4">
                                <div class="flex items-center gap-4">
                                    <div class="w-12 h-12 rounded-xl bg-base-300"></div>
                                    <div class="flex-1 space-y-2">
                                        <div class="h-4 bg-base-300 rounded w-1/3"></div>
                                        <div class="h-3 bg-base-300 rounded w-1/2"></div>
                                    </div>
                                    <div class="flex gap-2">
                                        <div class="h-8 w-24 bg-base-300 rounded-lg"></div>
                                        <div class="h-8 w-24 bg-base-300 rounded-lg"></div>
                                    </div>
                                </div>
                            </div>
                        </div>
                    </template>
                </div>
            </template>

            <!-- Client cards grid -->
            <template x-if="!loading || statuses.length > 0">
                <div class="grid gap-4">
                    <template x-if="statuses.length === 0">
                        <div class="card bg-base-100 shadow">
                            <div class="card-body py-12 text-center">
                                <div class="text-4xl mb-3">💻</div>
                                <p class="text-base-content/80" x-text="$store.global.t('no_clients')"></p>
                            </div>
                        </div>
                    </template>

                    <template x-for="client in statuses" :key="client.name">
                        <div class="card bg-base-100 shadow-md border border-base-200 hover:border-primary/30 transition-all duration-200">
                            <div class="card-body py-4 px-5">
                                <div class="flex items-center gap-4 flex-wrap">

                                    <!-- Icon + Name -->
                                    <div class="flex items-center gap-3 flex-1 min-w-0">
                                        <div class="flex-shrink-0 w-12 h-12 rounded-xl flex items-center justify-center text-2xl"
                                            :class="client.is_installed ? 'bg-primary/10' : 'bg-base-200'">
                                            <span x-text="$store.global.t('client_icon_' + client.name) || '💻'"></span>
                                        </div>
                                        <div class="min-w-0">
                                            <div class="flex items-center gap-2 flex-wrap">
                                                <span class="font-bold text-base" x-text="$store.global.t('client_name_' + client.name)"></span>
                                                <!-- Status badge -->
                                                <span class="badge badge-sm"
                                                    :class="getStatusBadge(client).cls"
                                                    x-text="getStatusBadge(client).label">
                                                </span>
                                                <!-- Backup badge -->
                                                <template x-if="client.has_backup">
                                                    <span class="badge badge-sm badge-info badge-outline gap-1">
                                                        <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/>
                                                        </svg>
                                                        <span x-text="$store.global.t('client_has_backup')"></span>
                                                    </span>
                                                </template>
                                            </div>
                                            <div class="flex items-center gap-2 mt-0.5">
                                                <p class="text-xs text-base-content/80" x-text="$store.global.t('client_desc_' + client.name)"></p>
                                                <template x-if="!client.is_installed">
                                                    <span class="text-xs text-warning flex items-center gap-1">
                                                        <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/>
                                                        </svg>
                                                        <span x-text="$store.global.t('client_not_installed')"></span>
                                                    </span>
                                                </template>
                                            </div>
                                            <!-- Codex Specific Hint -->
                                            <template x-if="client.name === 'codex'">
                                                <div class="mt-1.5 p-2 bg-info/10 rounded border border-info/20">
                                                    <p class="text-xs text-info-content flex items-start gap-1">
                                                        <svg class="w-3.5 h-3.5 flex-shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M12 2a10 10 0 100 20A10 10 0 0012 2z"></path></svg>
                                                        <span x-text="$store.global.lang === 'zh' ? '提示：请先正常登录 Codex 官方账号，然后再一键接入网关，这样即可正常使用 Codex 插件市场。' : 'Tip: Please log into your official Codex account before injecting the gateway config to keep the plugin marketplace enabled.'"></span>
                                                    </p>
                                                </div>
                                            </template>
                                            <!-- Error message -->
                                            <template x-if="client.error">
                                                <p class="text-xs text-error mt-1 flex items-center gap-1">
                                                    <svg class="w-3 h-3 flex-shrink-0" fill="currentColor" viewBox="0 0 20 20">
                                                        <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd"/>
                                                    </svg>
                                                    <span x-text="client.error"></span>
                                                </p>
                                            </template>
                                        </div>
                                    </div>

                                    <!-- Actions -->
                                    <div class="flex items-center gap-2 flex-shrink-0">
                                        <!-- Apply button -->
                                        <button
                                            @click="applyConfig(client.name)"
                                            :disabled="!client.is_installed || applyingClient === client.name"
                                            class="btn btn-sm btn-primary gap-2"
                                            :class="{'btn-outline': !client.is_configured, 'btn-success': client.is_configured}"
                                            :title="!client.is_installed ? $store.global.t('client_not_installed') : ''">
                                            <template x-if="applyingClient === client.name">
                                                <span class="loading loading-spinner loading-xs"></span>
                                            </template>
                                            <template x-if="applyingClient !== client.name">
                                                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z"/>
                                                </svg>
                                            </template>
                                            <span x-text="client.is_configured ? $store.global.t('btn_reapply_config') : $store.global.t('btn_apply_config')"></span>
                                        </button>

                                        <!-- Restore button -->
                                        <button
                                            @click="restoreConfig(client.name)"
                                            :disabled="(!client.has_backup && !client.is_configured) || restoringClient === client.name"
                                            class="btn btn-sm btn-warning btn-outline gap-2">
                                            <template x-if="restoringClient === client.name">
                                                <span class="loading loading-spinner loading-xs"></span>
                                            </template>
                                            <template x-if="restoringClient !== client.name">
                                                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6"/>
                                                </svg>
                                            </template>
                                            <span x-text="$store.global.t('btn_restore_config')"></span>
                                        </button>
                                    </div>

                                </div>

                                <!-- Configured: show what was injected -->
                                <template x-if="client.injected_url">
                                    <div class="mt-3 pt-3 border-t border-base-200">
                                        <div class="flex items-center gap-2 text-xs text-base-content/60">
                                            <svg class="w-3.5 h-3.5 text-success flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z"/>
                                            </svg>
                                            <span x-text="$store.global.lang === 'zh'
                                                ? '已注入代理地址：' + client.injected_url
                                                : 'Injected: ' + client.injected_url">
                                            </span>
                                        </div>
                                    </div>
                                </template>

                            </div>
                        </div>
                    </template>
                </div>
            </template>

            <!-- Help card -->
            <div class="card bg-base-100 border border-base-200 shadow-sm">
                <div class="card-body py-4">
                    <h3 class="font-semibold text-sm flex items-center gap-2">
                        <svg class="w-4 h-4 text-info" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M12 2a10 10 0 100 20A10 10 0 0012 2z"/>
                        </svg>
                        <span x-text="$store.global.t('client_how_it_works_title')"></span>
                    </h3>
                    <ol class="text-xs text-base-content/60 space-y-1 list-decimal list-inside mt-1">
                        <li x-text="$store.global.t('client_how_step1')"></li>
                        <li x-text="$store.global.t('client_how_step2')"></li>
                        <li x-text="$store.global.t('client_how_step3')"></li>
                        <li x-text="$store.global.t('client_how_step4')"></li>
                    </ol>
                    <div class="mt-3 p-3 bg-info/10 rounded-lg text-xs text-info-content border border-info/20 leading-relaxed">
                        <span x-html="$store.global.t('client_how_tip')"></span>
                    </div>
                </div>
            </div>

        </div>
    `
};
