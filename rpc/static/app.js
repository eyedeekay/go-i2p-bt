// go-i2p-bt RPC Server Web Interface JavaScript
// Provides basic interaction with the Transmission RPC API

class TransmissionRPC {
    constructor(baseUrl = window.location.origin) {
        this.baseUrl = baseUrl;
        this.rpcUrl = `${baseUrl}/transmission/rpc`;
        this.sessionId = null;
    }

    /**
     * Make a JSON-RPC request to the Transmission server
     * Handles session ID management automatically
     */
    async request(method, arguments = {}) {
        const requestBody = {
            method: method,
            arguments: arguments,
            tag: Date.now() // Simple request ID
        };

        const headers = {
            'Content-Type': 'application/json'
        };

        // Add session ID if we have one
        if (this.sessionId) {
            headers['X-Transmission-Session-Id'] = this.sessionId;
        }

        try {
            const response = await fetch(this.rpcUrl, {
                method: 'POST',
                headers: headers,
                body: JSON.stringify(requestBody)
            });

            // Handle session ID requirement (409 Conflict)
            if (response.status === 409) {
                const sessionId = response.headers.get('X-Transmission-Session-Id');
                if (sessionId) {
                    this.sessionId = sessionId;
                    // Retry with session ID
                    headers['X-Transmission-Session-Id'] = sessionId;
                    const retryResponse = await fetch(this.rpcUrl, {
                        method: 'POST',
                        headers: headers,
                        body: JSON.stringify(requestBody)
                    });
                    return await retryResponse.json();
                }
            }

            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }

            return await response.json();
        } catch (error) {
            console.error('RPC Request failed:', error);
            throw error;
        }
    }

    /**
     * Get session information from the server
     */
    async getSession() {
        return await this.request('session-get');
    }

    /**
     * Get statistics about the server
     */
    async getStats() {
        return await this.request('session-stats');
    }
}

// Web interface controller
class WebInterface {
    constructor() {
        this.rpc = new TransmissionRPC();
        this.initializeEventListeners();
        this.loadInitialData();
    }

    initializeEventListeners() {
        // Refresh statistics button
        const refreshBtn = document.getElementById('refresh-stats');
        if (refreshBtn) {
            refreshBtn.addEventListener('click', () => this.refreshStatistics());
        }

        // Test connection button
        const testBtn = document.getElementById('test-connection');
        if (testBtn) {
            testBtn.addEventListener('click', () => this.testConnection());
        }

        // Auto-refresh every 30 seconds
        setInterval(() => this.refreshStatistics(), 30000);
    }

    async loadInitialData() {
        await this.refreshStatistics();
        await this.checkAuthentication();
    }

    async refreshStatistics() {
        try {
            this.setStatus('server-status', 'Checking...', 'status-checking');
            
            const sessionResponse = await this.rpc.getSession();
            
            if (sessionResponse.result) {
                this.setStatus('server-status', 'Online', 'status-online');
                
                // Update session ID display
                if (this.rpc.sessionId) {
                    this.setValue('session-id', this.rpc.sessionId.substring(0, 16) + '...');
                }
                
                // Calculate uptime if available
                this.updateUptime();
            }
        } catch (error) {
            console.error('Failed to refresh statistics:', error);
            this.setStatus('server-status', 'Error', 'status-offline');
            this.setValue('session-id', 'Unknown');
            this.setValue('uptime', 'Unknown');
        }
    }

    async testConnection() {
        const testBtn = document.getElementById('test-connection');
        const originalText = testBtn.textContent;
        
        try {
            testBtn.disabled = true;
            testBtn.textContent = 'Testing...';
            
            const response = await this.rpc.getSession();
            
            if (response.result) {
                this.showNotification('Connection test successful!', 'success');
            } else {
                this.showNotification('Connection test failed: No result', 'error');
            }
        } catch (error) {
            this.showNotification(`Connection test failed: ${error.message}`, 'error');
        } finally {
            testBtn.disabled = false;
            testBtn.textContent = originalText;
        }
    }

    async checkAuthentication() {
        try {
            const response = await this.rpc.getSession();
            
            if (response.result) {
                this.setValue('auth-status', 'Connected');
            } else {
                this.setValue('auth-status', 'Authentication required');
            }
        } catch (error) {
            if (error.message.includes('401')) {
                this.setValue('auth-status', 'Authentication required');
            } else {
                this.setValue('auth-status', 'Connection error');
            }
        }
    }

    updateUptime() {
        // Simple uptime calculation based on when the page loaded
        const startTime = Date.now() - (performance.now());
        const uptime = Date.now() - startTime;
        const uptimeStr = this.formatDuration(uptime);
        this.setValue('uptime', uptimeStr);
    }

    formatDuration(milliseconds) {
        const seconds = Math.floor(milliseconds / 1000);
        const minutes = Math.floor(seconds / 60);
        const hours = Math.floor(minutes / 60);
        const days = Math.floor(hours / 24);

        if (days > 0) {
            return `${days}d ${hours % 24}h ${minutes % 60}m`;
        } else if (hours > 0) {
            return `${hours}h ${minutes % 60}m ${seconds % 60}s`;
        } else if (minutes > 0) {
            return `${minutes}m ${seconds % 60}s`;
        } else {
            return `${seconds}s`;
        }
    }

    setValue(elementId, value) {
        const element = document.getElementById(elementId);
        if (element) {
            element.textContent = value;
        }
    }

    setStatus(elementId, status, className) {
        const element = document.getElementById(elementId);
        if (element) {
            element.textContent = status;
            // Remove existing status classes
            element.classList.remove('status-online', 'status-offline', 'status-checking');
            // Add new status class
            if (className) {
                element.classList.add(className);
            }
        }
    }

    showNotification(message, type = 'info') {
        // Create notification element
        const notification = document.createElement('div');
        notification.className = `notification notification-${type}`;
        notification.textContent = message;
        notification.style.cssText = `
            position: fixed;
            top: 20px;
            right: 20px;
            padding: 15px 20px;
            background-color: ${type === 'success' ? '#27ae60' : type === 'error' ? '#e74c3c' : '#3498db'};
            color: white;
            border-radius: 5px;
            box-shadow: 0 4px 15px rgba(0, 0, 0, 0.2);
            z-index: 1000;
            opacity: 0;
            transition: opacity 0.3s ease;
        `;

        document.body.appendChild(notification);

        // Animate in
        setTimeout(() => {
            notification.style.opacity = '1';
        }, 10);

        // Remove after 3 seconds
        setTimeout(() => {
            notification.style.opacity = '0';
            setTimeout(() => {
                if (notification.parentNode) {
                    notification.parentNode.removeChild(notification);
                }
            }, 300);
        }, 3000);
    }
}

// Initialize the web interface when the page loads
document.addEventListener('DOMContentLoaded', () => {
    new WebInterface();
});

// Utility functions for potential future use
window.TransmissionAPI = {
    // Expose the RPC class for external use
    TransmissionRPC: TransmissionRPC,
    
    // Quick test function
    testAPI: async function() {
        const rpc = new TransmissionRPC();
        try {
            const session = await rpc.getSession();
            console.log('API Test successful:', session);
            return session;
        } catch (error) {
            console.error('API Test failed:', error);
            throw error;
        }
    }
};