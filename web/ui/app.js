import './store.js?v=3';
import UsageBilling from './components/UsageBilling.js?v=3';
import AIAccounts from './components/AIAccounts.js?v=3';
import SmartRouting from './components/SmartRouting.js?v=3';
import ClientAccess from './components/ClientAccess.js?v=3';
import Settings from './components/Settings.js?v=3';
import Logs from './components/Logs.js?v=3';

const components = [
    UsageBilling,
    AIAccounts,
    SmartRouting,
    ClientAccess,
    Settings,
    Logs
];

document.addEventListener('alpine:init', () => {
    const container = document.getElementById('components-container');
    
    components.forEach(comp => {
        // 1. Register Alpine data function
        if (comp.name && comp.setup) {
            Alpine.data(comp.name, () => comp.setup());
        }
        
        // 2. Inject template string into DOM
        if (comp.template) {
            // We wrap it so we can easily attach x-data
            const wrapper = document.createElement('div');
            wrapper.setAttribute('x-data', comp.name ? `${comp.name}()` : '{}');
            wrapper.innerHTML = comp.template;
            container.appendChild(wrapper);
        }
    });
});
