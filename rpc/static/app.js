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
  async request(method, args = {}) {
    const requestBody = {
      method: method,
      arguments: args,
      tag: Date.now(), // Simple request ID
    };

    const headers = {
      "Content-Type": "application/json",
    };

    // Add session ID if we have one
    if (this.sessionId) {
      headers["X-Transmission-Session-Id"] = this.sessionId;
    }

    try {
      const response = await fetch(this.rpcUrl, {
        method: "POST",
        headers: headers,
        body: JSON.stringify(requestBody),
      });

      // Handle session ID requirement (409 Conflict)
      if (response.status === 409) {
        const sessionId = response.headers.get("X-Transmission-Session-Id");
        if (sessionId) {
          this.sessionId = sessionId;
          // Retry with session ID
          headers["X-Transmission-Session-Id"] = sessionId;
          const retryResponse = await fetch(this.rpcUrl, {
            method: "POST",
            headers: headers,
            body: JSON.stringify(requestBody),
          });
          return await retryResponse.json();
        }
      }

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${response.statusText}`);
      }

      return await response.json();
    } catch (error) {
      console.error("RPC Request failed:", error);
      throw error;
    }
  }

  /**
   * Get session information from the server
   */
  async getSession() {
    return await this.request("session-get");
  }

  /**
   * Get statistics about the server
   */
  async getStats() {
    return await this.request("session-stats");
  }

  /**
   * Get torrent list with specified fields
   */
  async getTorrents(fields = ["id", "name", "status", "percentDone", "totalSize", "rateDownload", "rateUpload", "eta", "error", "errorString"]) {
    return await this.request("torrent-get", { fields: fields });
  }

  /**
   * Start torrents
   */
  async startTorrents(ids = []) {
    return await this.request("torrent-start", { ids: ids });
  }

  /**
   * Stop torrents
   */
  async stopTorrents(ids = []) {
    return await this.request("torrent-stop", { ids: ids });
  }

  /**
   * Remove torrents
   */
  async removeTorrents(ids = [], deleteLocalData = false) {
    return await this.request("torrent-remove", { 
      ids: ids, 
      "delete-local-data": deleteLocalData 
    });
  }
}

// Web interface controller
class WebInterface {
  constructor() {
    this.rpc = new TransmissionRPC();
    this.pageLoadTime = Date.now();
    this.torrents = [];
    this.sortColumn = 'name';
    this.sortDirection = 'asc';
    this.statusFilter = '';
    this.nameFilter = '';
    this.initializeEventListeners();
    this.loadInitialData();
  }

  initializeEventListeners() {
    // Refresh statistics button
    const refreshBtn = document.getElementById("refresh-stats");
    if (refreshBtn) {
      refreshBtn.addEventListener("click", () => this.refreshStatistics());
    }

    // Test connection button
    const testBtn = document.getElementById("test-connection");
    if (testBtn) {
      testBtn.addEventListener("click", () => this.testConnection());
    }

    // File upload form
    const uploadForm = document.getElementById("torrent-upload-form");
    if (uploadForm) {
      uploadForm.addEventListener("submit", (e) => this.handleFileUpload(e));
    }

    // File input change handler
    const fileInput = document.getElementById("torrent-file");
    if (fileInput) {
      fileInput.addEventListener("change", (e) => this.handleFileSelect(e));
    }

    // Torrent management buttons
    const refreshTorrentsBtn = document.getElementById("refresh-torrents");
    if (refreshTorrentsBtn) {
      refreshTorrentsBtn.addEventListener("click", () => this.refreshTorrentList());
    }

    const startAllBtn = document.getElementById("start-all");
    if (startAllBtn) {
      startAllBtn.addEventListener("click", () => this.startAllTorrents());
    }

    const stopAllBtn = document.getElementById("stop-all");
    if (stopAllBtn) {
      stopAllBtn.addEventListener("click", () => this.stopAllTorrents());
    }

    // Filter controls
    const statusFilter = document.getElementById("status-filter");
    if (statusFilter) {
      statusFilter.addEventListener("change", (e) => this.updateStatusFilter(e.target.value));
    }

    const nameFilter = document.getElementById("name-filter");
    if (nameFilter) {
      nameFilter.addEventListener("input", (e) => this.updateNameFilter(e.target.value));
    }

    // Table sorting
    this.initializeTableSorting();

    // Auto-refresh every 30 seconds
    setInterval(() => {
      this.refreshStatistics();
      this.refreshTorrentList();
    }, 30000);
  }

  async loadInitialData() {
    await this.refreshStatistics();
    await this.checkAuthentication();
    await this.refreshTorrentList();
  }

  async refreshStatistics() {
    try {
      this.setStatus("server-status", "Checking...", "status-checking");

      const sessionResponse = await this.rpc.getSession();

      if (sessionResponse.result) {
        this.setStatus("server-status", "Online", "status-online");

        // Update session ID display
        if (this.rpc.sessionId) {
          if (this.rpc.sessionId.length > 16) {
            this.setValue(
              "session-id",
              this.rpc.sessionId.substring(0, 16) + "..."
            );
          } else {
            this.setValue("session-id", this.rpc.sessionId);
          }
        }

        // Calculate uptime if available
        this.updateUptime();
      }
    } catch (error) {
      console.error("Failed to refresh statistics:", error);
      this.setStatus("server-status", "Error", "status-offline");
      this.setValue("session-id", "Unknown");
      this.setValue("uptime", "Unknown");
    }
  }
  async testConnection() {
    const testBtn = document.getElementById("test-connection");
    if (!testBtn) {
      console.error("Element with id 'test-connection' not found.");
      return;
    }
    const originalText = testBtn.textContent;

    try {
      testBtn.disabled = true;
      testBtn.textContent = "Testing...";

      const response = await this.rpc.getSession();

      if (response.result) {
        this.showNotification("Connection test successful!", "success");
      } else {
        this.showNotification("Connection test failed: No result", "error");
      }
    } catch (error) {
      this.showNotification(
        `Connection test failed: ${error.message}`,
        "error"
      );
    } finally {
      testBtn.disabled = false;
      testBtn.textContent = originalText;
    }
  }

  async checkAuthentication() {
    try {
      const response = await this.rpc.getSession();

      if (response.result) {
        this.setValue("auth-status", "Connected");
      } else {
        this.setValue("auth-status", "Authentication required");
      }
    } catch (error) {
      if (error.message.includes("401")) {
        this.setValue("auth-status", "Authentication required");
      } else {
        this.setValue("auth-status", "Connection error");
      }
    }
  }

  updateUptime() {
    // Uptime calculation based on when the page loaded
    const uptime = Date.now() - this.pageLoadTime;
    const uptimeStr = this.formatDuration(uptime);
    this.setValue("uptime", uptimeStr);
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
      element.classList.remove(
        "status-online",
        "status-offline",
        "status-checking"
      );
      // Add new status class
      if (className) {
        element.classList.add(className);
      }
    }
  }

  async refreshTorrentList() {
    const loadingElement = document.getElementById("torrent-list-loading");
    const emptyElement = document.getElementById("torrent-list-empty");
    const tableElement = document.getElementById("torrent-list");

    try {
      // Show loading state
      if (loadingElement) loadingElement.style.display = "block";
      if (emptyElement) emptyElement.style.display = "none";
      if (tableElement) tableElement.style.display = "none";

      const response = await this.rpc.getTorrents();
      
      if (response.result === 'success' && response.arguments && response.arguments.torrents) {
        this.torrents = response.arguments.torrents;
        this.renderTorrentList();
      } else {
        this.showTorrentListEmpty();
      }
    } catch (error) {
      console.error("Failed to refresh torrent list:", error);
      this.showNotification("Failed to load torrent list", "error");
      this.showTorrentListEmpty();
    } finally {
      if (loadingElement) loadingElement.style.display = "none";
    }
  }

  renderTorrentList() {
    const tableElement = document.getElementById("torrent-list");
    const tbody = document.getElementById("torrent-list-body");
    const emptyElement = document.getElementById("torrent-list-empty");

    if (!tbody || !tableElement) return;

    // Filter and sort torrents
    let filteredTorrents = this.filterTorrents(this.torrents);
    filteredTorrents = this.sortTorrents(filteredTorrents);

    if (filteredTorrents.length === 0) {
      this.showTorrentListEmpty();
      return;
    }

    // Hide empty message and show table
    if (emptyElement) emptyElement.style.display = "none";
    tableElement.style.display = "table";

    // Clear existing rows
    tbody.innerHTML = "";

    // Add torrent rows
    filteredTorrents.forEach(torrent => {
      const row = this.createTorrentRow(torrent);
      tbody.appendChild(row);
    });
  }

  createTorrentRow(torrent) {
    const row = document.createElement("tr");
    row.dataset.torrentId = torrent.id;

    const statusClass = this.getTorrentStatusClass(torrent.status);
    const statusText = this.getTorrentStatusText(torrent.status);

    row.innerHTML = `
      <td class="torrent-name" title="${this.escapeHtml(torrent.name || 'Unknown')}">${this.escapeHtml(torrent.name || 'Unknown')}</td>
      <td><span class="torrent-status ${statusClass}">${statusText}</span></td>
      <td>
        <div class="progress-container">
          <div class="progress-bar-torrent">
            <div class="progress-fill-torrent" style="width: ${(torrent.percentDone * 100).toFixed(1)}%"></div>
          </div>
          <div class="progress-text-torrent">${(torrent.percentDone * 100).toFixed(1)}%</div>
        </div>
      </td>
      <td class="torrent-size">${this.formatBytes(torrent.totalSize || 0)}</td>
      <td class="torrent-speed">${this.formatSpeed(torrent.rateDownload || 0)}</td>
      <td class="torrent-speed">${this.formatSpeed(torrent.rateUpload || 0)}</td>
      <td class="torrent-eta">${this.formatETA(torrent.eta || -1)}</td>
      <td class="torrent-actions">
        <div class="action-buttons-row">
          <button class="btn btn-primary btn-small" onclick="webInterface.startTorrent(${torrent.id})">Start</button>
          <button class="btn btn-warning btn-small" onclick="webInterface.stopTorrent(${torrent.id})">Stop</button>
          <button class="btn btn-danger btn-small" onclick="webInterface.removeTorrent(${torrent.id})">Remove</button>
        </div>
      </td>
    `;

    return row;
  }

  showTorrentListEmpty() {
    const tableElement = document.getElementById("torrent-list");
    const emptyElement = document.getElementById("torrent-list-empty");

    if (tableElement) tableElement.style.display = "none";
    if (emptyElement) emptyElement.style.display = "block";
  }

  async startTorrent(id) {
    try {
      const response = await this.rpc.startTorrents([id]);
      if (response.result === 'success') {
        this.showNotification("Torrent started successfully", "success");
        await this.refreshTorrentList();
      } else {
        this.showNotification("Failed to start torrent", "error");
      }
    } catch (error) {
      console.error("Failed to start torrent:", error);
      this.showNotification(`Failed to start torrent: ${error.message}`, "error");
    }
  }

  async stopTorrent(id) {
    try {
      const response = await this.rpc.stopTorrents([id]);
      if (response.result === 'success') {
        this.showNotification("Torrent stopped successfully", "success");
        await this.refreshTorrentList();
      } else {
        this.showNotification("Failed to stop torrent", "error");
      }
    } catch (error) {
      console.error("Failed to stop torrent:", error);
      this.showNotification(`Failed to stop torrent: ${error.message}`, "error");
    }
  }

  async removeTorrent(id) {
    if (!confirm("Are you sure you want to remove this torrent?")) {
      return;
    }

    try {
      const response = await this.rpc.removeTorrents([id], false);
      if (response.result === 'success') {
        this.showNotification("Torrent removed successfully", "success");
        await this.refreshTorrentList();
      } else {
        this.showNotification("Failed to remove torrent", "error");
      }
    } catch (error) {
      console.error("Failed to remove torrent:", error);
      this.showNotification(`Failed to remove torrent: ${error.message}`, "error");
    }
  }

  async startAllTorrents() {
    try {
      const response = await this.rpc.startTorrents();
      if (response.result === 'success') {
        this.showNotification("All torrents started successfully", "success");
        await this.refreshTorrentList();
      } else {
        this.showNotification("Failed to start all torrents", "error");
      }
    } catch (error) {
      console.error("Failed to start all torrents:", error);
      this.showNotification(`Failed to start all torrents: ${error.message}`, "error");
    }
  }

  async stopAllTorrents() {
    if (!confirm("Are you sure you want to stop all torrents?")) {
      return;
    }

    try {
      const response = await this.rpc.stopTorrents();
      if (response.result === 'success') {
        this.showNotification("All torrents stopped successfully", "success");
        await this.refreshTorrentList();
      } else {
        this.showNotification("Failed to stop all torrents", "error");
      }
    } catch (error) {
      console.error("Failed to stop all torrents:", error);
      this.showNotification(`Failed to stop all torrents: ${error.message}`, "error");
    }
  }

  // Filtering and sorting methods
  filterTorrents(torrents) {
    return torrents.filter(torrent => {
      // Status filter
      if (this.statusFilter) {
        const torrentStatus = this.getTorrentStatusText(torrent.status).toLowerCase();
        if (!torrentStatus.includes(this.statusFilter)) {
          return false;
        }
      }

      // Name filter
      if (this.nameFilter) {
        const torrentName = (torrent.name || '').toLowerCase();
        if (!torrentName.includes(this.nameFilter.toLowerCase())) {
          return false;
        }
      }

      return true;
    });
  }

  sortTorrents(torrents) {
    return [...torrents].sort((a, b) => {
      let aVal = a[this.sortColumn];
      let bVal = b[this.sortColumn];

      // Handle special cases
      if (this.sortColumn === 'name') {
        aVal = (aVal || '').toLowerCase();
        bVal = (bVal || '').toLowerCase();
      } else if (this.sortColumn === 'progress') {
        aVal = a.percentDone || 0;
        bVal = b.percentDone || 0;
      } else if (this.sortColumn === 'size') {
        aVal = a.totalSize || 0;
        bVal = b.totalSize || 0;
      } else if (this.sortColumn === 'downloadRate') {
        aVal = a.rateDownload || 0;
        bVal = b.rateDownload || 0;
      } else if (this.sortColumn === 'uploadRate') {
        aVal = a.rateUpload || 0;
        bVal = b.rateUpload || 0;
      }

      if (aVal < bVal) return this.sortDirection === 'asc' ? -1 : 1;
      if (aVal > bVal) return this.sortDirection === 'asc' ? 1 : -1;
      return 0;
    });
  }

  updateStatusFilter(value) {
    this.statusFilter = value;
    this.renderTorrentList();
  }

  updateNameFilter(value) {
    this.nameFilter = value;
    this.renderTorrentList();
  }

  initializeTableSorting() {
    const headers = document.querySelectorAll('.torrent-table th.sortable');
    headers.forEach(header => {
      header.addEventListener('click', () => {
        const column = header.dataset.sort;
        
        if (this.sortColumn === column) {
          this.sortDirection = this.sortDirection === 'asc' ? 'desc' : 'asc';
        } else {
          this.sortColumn = column;
          this.sortDirection = 'asc';
        }

        // Update header classes
        headers.forEach(h => {
          h.classList.remove('sort-asc', 'sort-desc');
        });
        header.classList.add(this.sortDirection === 'asc' ? 'sort-asc' : 'sort-desc');

        this.renderTorrentList();
      });
    });
  }

  // Utility methods
  getTorrentStatusClass(status) {
    switch (status) {
      case 4: return 'status-downloading';
      case 6: return 'status-seeding';
      case 0: return 'status-paused';
      case 2: return 'status-checking';
      default: return 'status-error';
    }
  }

  getTorrentStatusText(status) {
    switch (status) {
      case 0: return 'Paused';
      case 1: return 'Queued';
      case 2: return 'Checking';
      case 3: return 'Queued DL';
      case 4: return 'Downloading';
      case 5: return 'Queued Seed';
      case 6: return 'Seeding';
      default: return 'Unknown';
    }
  }

  formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  }

  formatSpeed(bytesPerSecond) {
    if (bytesPerSecond === 0) return '0 B/s';
    return this.formatBytes(bytesPerSecond) + '/s';
  }

  formatETA(seconds) {
    if (seconds < 0 || seconds === Infinity) return '∞';
    if (seconds < 60) return `${Math.round(seconds)}s`;
    if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
    if (seconds < 86400) return `${Math.round(seconds / 3600)}h`;
    return `${Math.round(seconds / 86400)}d`;
  }

  escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
  }

  async handleFileUpload(event) {
    event.preventDefault();

    const fileInput = document.getElementById("torrent-file");
    const pausedInput = document.getElementById("paused-upload");
    const uploadBtn = document.getElementById("upload-btn");
    const progressBar = document.getElementById("upload-progress");

    if (!fileInput.files || fileInput.files.length === 0) {
      this.showNotification("Please select a .torrent file", "error");
      return;
    }

    const file = fileInput.files[0];

    // Validate file extension
    if (!file.name.toLowerCase().endsWith(".torrent")) {
      this.showNotification("Please select a valid .torrent file", "error");
      return;
    }

    // Validate file size (10MB max)
    const maxSize = 10 * 1024 * 1024; // 10MB
    if (file.size > maxSize) {
      this.showNotification("File too large (max 10MB)", "error");
      return;
    }

    try {
      // Show progress
      uploadBtn.style.display = "none";
      progressBar.style.display = "block";

      // Create form data
      const formData = new FormData();
      formData.append("torrent", file);
      formData.append("paused", pausedInput.checked ? "true" : "false");

      // Upload file
      const response = await fetch("/upload", {
        method: "POST",
        body: formData,
      });

      const result = await response.json();

      if (result.success) {
        this.showNotification(
          `Torrent "${file.name}" uploaded successfully!`,
          "success"
        );
        this.resetUploadForm();
        // Refresh stats and torrent list to show new torrent
        await this.refreshStatistics();
        await this.refreshTorrentList();
      } else {
        this.showNotification(
          `Upload failed: ${result.error || "Unknown error"}`,
          "error"
        );
      }
    } catch (error) {
      console.error("Upload error:", error);
      this.showNotification(
        `Upload failed: ${error.message}`,
        "error"
      );
    } finally {
      // Hide progress
      progressBar.style.display = "none";
      uploadBtn.style.display = "block";
    }
  }

  handleFileSelect(event) {
    const fileInput = event.target;
    const fileLabel = document.getElementById("file-label-text");
    const wrapper = fileInput.parentElement;

    if (fileInput.files && fileInput.files.length > 0) {
      const file = fileInput.files[0];
      fileLabel.textContent = file.name;
      wrapper.classList.add("has-file");
    } else {
      fileLabel.textContent = "Choose .torrent file";
      wrapper.classList.remove("has-file");
    }
  }

  resetUploadForm() {
    const fileInput = document.getElementById("torrent-file");
    const pausedInput = document.getElementById("paused-upload");
    const fileLabel = document.getElementById("file-label-text");
    const wrapper = fileInput.parentElement;

    fileInput.value = "";
    pausedInput.checked = false;
    fileLabel.textContent = "Choose .torrent file";
    wrapper.classList.remove("has-file");
  }

  showNotification(message, type = "info") {
    // Create notification element
    const notification = document.createElement("div");
    notification.className = `notification notification-${type}`;
    notification.textContent = message;
    notification.style.cssText = `
            position: fixed;
            top: 20px;
            right: 20px;
            padding: 15px 20px;
            background-color: ${
              type === "success"
                ? "#27ae60"
                : type === "error"
                ? "#e74c3c"
                : "#3498db"
            };
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
      notification.style.opacity = "1";
    }, 10);

    // Remove after 3 seconds
    setTimeout(() => {
      notification.style.opacity = "0";
      setTimeout(() => {
        if (notification.parentNode) {
          notification.parentNode.removeChild(notification);
        }
      }, 300);
    }, 3000);
  }
}

// Initialize the web interface when the page loads
document.addEventListener("DOMContentLoaded", () => {
  window.webInterface = new WebInterface();
});

// Utility functions for potential future use
window.TransmissionAPI = {
  // Expose the RPC class for external use
  TransmissionRPC: TransmissionRPC,

  // Quick test function
  testAPI: async function () {
    const rpc = new TransmissionRPC();
    try {
      const session = await rpc.getSession();
      console.log("API Test successful:", session);
      return session;
    } catch (error) {
      console.error("API Test failed:", error);
      throw error;
    }
  },
};
