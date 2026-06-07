export default {
    name: 'settingsComponent',
    setup() {
        return {
            activeSubTab: 'general',
            async fetchSettings() {
                if (Alpine.store('global').currentTab !== 'settings') return;
                try {
                    const res = await fetch('/api/admin/settings');
                    const json = await res.json();
                    if (json && Object.keys(json).length > 0) {
                        const current = Alpine.store('global').settings;
                        Alpine.store('global').settings = {
                            ...current,
                            ...json,
                            breaker: {
                                ...current.breaker,
                                ...(json.breaker || {})
                            }
                        };
                    }
                } catch (e) { console.error(e) }
            },

            async saveGeneralSettings() {
                const gStore = Alpine.store('global');
                if (gStore.settings.breaker.failure_threshold < 0 || 
                    gStore.settings.breaker.failure_window_seconds < 0 || 
                    gStore.settings.breaker.initial_cooldown_seconds < 0 || 
                    gStore.settings.breaker.max_cooldown_seconds < 0) {
                    gStore.showToast(gStore.t('err_negative'), 'error');
                    return;
                }
                
                try {
                    const payload = {
                        listen_addr: gStore.settings.listen_addr,
                        update_proxy_channel: gStore.settings.update_proxy_channel,
                        initial_cooldown_seconds: gStore.settings.breaker.initial_cooldown_seconds,
                        max_cooldown_seconds: gStore.settings.breaker.max_cooldown_seconds,
                        failure_threshold: gStore.settings.breaker.failure_threshold,
                        failure_window_seconds: gStore.settings.breaker.failure_window_seconds
                    };
                    const res = await fetch('/api/admin/settings', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(payload)
                    });
                    if (res.ok) {
                        gStore.showToast(gStore.t('settings_saved'));
                    } else {
                        gStore.showToast(gStore.t('save_failed'), 'error');
                    }
                } catch(e) {
                    gStore.showToast(gStore.t('network_error'), 'error');
                }
            },

            async saveOAuthSettings() {
                const gStore = Alpine.store('global');
                try {
                    const payload = {
                        google_oauth_client_id: gStore.settings.google_oauth_client_id || '',
                        google_oauth_client_secret: gStore.settings.google_oauth_client_secret || ''
                    };
                    const res = await fetch('/api/admin/settings', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(payload)
                    });
                    if (res.ok) {
                        gStore.showToast(gStore.t('settings_saved'));
                    } else {
                        gStore.showToast(gStore.t('save_failed'), 'error');
                    }
                } catch(e) {
                    gStore.showToast(gStore.t('network_error'), 'error');
                }
            },

            resetSettings() {
                const gStore = Alpine.store('global');
                if(!confirm(gStore.lang === 'zh' ? '确定要恢复系统默认设置吗？' : 'Are you sure you want to reset to default settings?')) return;
                gStore.settings = {
                    listen_addr: '127.0.0.1:27777',
                    update_proxy_channel: '',
                    breaker: {
                        initial_cooldown_seconds: 60,
                        max_cooldown_seconds: 3600,
                        failure_threshold: 3,
                        failure_window_seconds: 120
                    },
                    google_oauth_client_id: '',
                    google_oauth_client_secret: ''
                };
            },

            get redirectUri() {
                return window.location.protocol + '//' + window.location.host + '/api/admin/oauth/google/callback';
            },

            handleOAuthJsonUpload(event) {
                const file = event.target.files[0];
                if (!file) return;
                
                const gStore = Alpine.store('global');
                const reader = new FileReader();
                reader.onload = (e) => {
                    try {
                        const json = JSON.parse(e.target.result);
                        let creds = null;
                        if (json.web) creds = json.web;
                        else if (json.installed) creds = json.installed;
                        else if (json.client_id && json.client_secret) creds = json;
                        
                        if (creds && creds.client_id && creds.client_secret) {
                            gStore.settings.google_oauth_client_id = creds.client_id;
                            gStore.settings.google_oauth_client_secret = creds.client_secret;
                            gStore.showToast(gStore.t('oauth_parse_success'), 'success');
                        } else {
                            gStore.showToast(gStore.t('oauth_parse_failed'), 'error');
                        }
                    } catch (err) {
                        gStore.showToast(gStore.t('oauth_parse_failed'), 'error');
                    }
                    event.target.value = ''; // Reset input
                };
                reader.readAsText(file);
            },

            clearOAuth() {
                const gStore = Alpine.store('global');
                gStore.settings.google_oauth_client_id = '';
                gStore.settings.google_oauth_client_secret = '';
                this.saveOAuthSettings();
            },

            init() {
                this.fetchSettings();
                
                this.$watch('$store.global.currentTab', (newTab) => {
                    if (newTab === 'settings') {
                        this.fetchSettings();
                    }
                });
            }
        };
    },
    template: `
        <div x-show="$store.global.currentTab === 'settings'" class="max-w-3xl mx-auto w-full">
            <div class="flex items-center justify-between mb-6">
                <h2 class="text-3xl font-bold" x-text="$store.global.t('tab_settings_title')"></h2>
            </div>
            
            <div class="tabs tabs-boxed bg-base-200 mb-6 p-1 gap-1 inline-flex">
                <a class="tab tab-lg px-8 transition-all" :class="{ 'tab-active font-bold bg-base-100 shadow': activeSubTab === 'general' }" @click="activeSubTab = 'general'" x-text="$store.global.t('settings_basic')"></a>
                <a class="tab tab-lg px-8 transition-all" :class="{ 'tab-active font-bold bg-base-100 shadow': activeSubTab === 'oauth' }" @click="activeSubTab = 'oauth'" x-text="$store.global.t('section_google_oauth')"></a>
            </div>

            <div class="card bg-base-100 shadow-lg p-8 space-y-8" x-show="activeSubTab === 'general'" x-transition:enter="transition ease-out duration-300" x-transition:enter-start="opacity-0 translate-y-4" x-transition:enter-end="opacity-100 translate-y-0">
                <div>
                    <h3 class="text-xl font-bold mb-4 border-b border-base-300 pb-2" x-text="$store.global.t('settings_basic')"></h3>
                    <div class="space-y-4">
                        <label class="form-control w-full">
                            <div class="label"><span class="label-text font-medium" x-text="$store.global.t('listen_addr_label')"></span></div>
                            <input name="store_global_settings_listen_addr" x-model="$store.global.settings.listen_addr" type="text" class="input input-bordered w-full">
                            <div class="label"><span class="label-text-alt text-base-content/50" x-text="$store.global.t('listen_addr_hint')"></span></div>
                        </label>
                        <label class="form-control w-full">
                            <div class="label"><span class="label-text font-medium" x-text="$store.global.t('proxy_channel_label')"></span></div>
                            <input list="proxy-channel-options" name="store_global_settings_update_proxy_channel" x-model="$store.global.settings.update_proxy_channel" type="text" class="input input-bordered w-full" :placeholder="$store.global.t('proxy_auto')">
                            <datalist id="proxy-channel-options">
                                <option value=""></option>
                                <option value="disable"></option>
                                <option value="https://ghproxy.net/"></option>
                            </datalist>
                            <div class="label"><span class="label-text-alt text-base-content/50" x-text="$store.global.t('proxy_channel_hint')"></span></div>
                        </label>
                    </div>
                </div>

                <div>
                    <h3 class="text-xl font-bold mb-4 border-b border-base-300 pb-2" x-text="$store.global.t('cb_rules')"></h3>
                    <div class="grid grid-cols-2 gap-4">
                        <label class="form-control w-full">
                            <div class="label"><span class="label-text" x-text="$store.global.t('cb_threshold_label')"></span></div>
                            <input name="store_global_settings_breaker_failure_threshold" x-model.number="$store.global.settings.breaker.failure_threshold" type="number" min="0" class="input input-bordered w-full">
                        </label>
                        <label class="form-control w-full">
                            <div class="label"><span class="label-text" x-text="$store.global.t('cb_failure_window')"></span></div>
                            <input name="store_global_settings_breaker_failure_window_seconds" x-model.number="$store.global.settings.breaker.failure_window_seconds" type="number" min="0" class="input input-bordered w-full">
                        </label>
                        <label class="form-control w-full">
                            <div class="label"><span class="label-text" x-text="$store.global.t('cb_initial_cooldown')"></span></div>
                            <input name="store_global_settings_breaker_initial_cooldown_seconds" x-model.number="$store.global.settings.breaker.initial_cooldown_seconds" type="number" min="0" class="input input-bordered w-full">
                        </label>
                        <label class="form-control w-full">
                            <div class="label"><span class="label-text" x-text="$store.global.t('cb_max_cooldown')"></span></div>
                            <input name="store_global_settings_breaker_max_cooldown_seconds" x-model.number="$store.global.settings.breaker.max_cooldown_seconds" type="number" min="0" class="input input-bordered w-full">
                        </label>
                    </div>
                </div>

                <div class="pt-4 flex justify-end gap-3">
                    <button @click="resetSettings" class="btn" x-text="$store.global.t('btn_reset_default')"></button>
                    <button @click="saveGeneralSettings" class="btn btn-primary shadow-lg shadow-primary/20" x-text="$store.global.t('btn_save_settings')"></button>
                </div>
            </div>
            
            <div class="card bg-base-100 shadow-lg p-8 space-y-8" x-show="activeSubTab === 'oauth'" x-transition:enter="transition ease-out duration-300" x-transition:enter-start="opacity-0 translate-y-4" x-transition:enter-end="opacity-100 translate-y-0" style="display:none;">
                <div>
                    <h3 class="text-xl font-bold mb-4 border-b border-base-300 pb-2" x-text="$store.global.t('section_google_oauth')"></h3>
                    <p class="text-sm text-base-content/60 mb-4 leading-loose">
                        <span x-text="$store.global.t('oauth_hint_1')"></span>
                        <a href="https://console.cloud.google.com/apis/credentials" target="_blank" class="link link-info" x-text="$store.global.t('oauth_hint_link')"></a>
                        <span x-text="$store.global.t('oauth_hint_2')"></span><br>
                        <span x-text="$store.global.t('oauth_hint_3')"></span>
                        <code class="bg-base-200 px-2 py-1 rounded text-success ml-2" x-text="redirectUri"></code>
                    </p>
                    <div class="space-y-4">
                        <label class="form-control w-full">
                            <div class="label"><span class="label-text font-medium" x-text="$store.global.t('oauth_upload_json')"></span></div>
                            <div>
                                <label class="btn btn-primary btn-outline shadow-sm cursor-pointer">
                                    <svg class="w-4 h-4 mr-1 inline" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12"></path></svg>
                                    <span x-text="$store.global.t('btn_upload_json')"></span>
                                    <input type="file" accept="application/json,.json" @change="handleOAuthJsonUpload" class="hidden">
                                </label>
                            </div>
                        </label>
                        
                        <div x-show="$store.global.settings.google_oauth_client_id" class="p-4 bg-success/10 border border-success/20 rounded-lg text-sm max-w-md relative">
                            <button @click="clearOAuth" class="btn btn-xs btn-circle btn-ghost absolute top-2 right-2 text-base-content/50 hover:text-error" title="Clear">✕</button>
                            <div class="flex items-center gap-2 text-success mb-2">
                                <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
                                    <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clip-rule="evenodd" />
                                </svg>
                                <span class="font-bold" x-text="$store.global.t('oauth_configured')"></span>
                            </div>
                            <div class="font-mono text-base-content/70 break-all">
                                Client ID: <span x-text="$store.global.settings.google_oauth_client_id"></span>
                            </div>
                            <div class="mt-2 text-xs text-base-content/50" x-text="$store.global.t('oauth_client_secret_hidden')"></div>
                        </div>
                    </div>
                </div>
                
                <div class="pt-4 flex justify-end gap-3">
                    <button @click="saveOAuthSettings" class="btn btn-primary shadow-lg shadow-primary/20" x-text="$store.global.t('btn_save_settings')"></button>
                </div>
            </div>
        </div>
    `
};
