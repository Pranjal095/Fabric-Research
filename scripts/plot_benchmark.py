import matplotlib.pyplot as plt
import csv
import os

RESULTS_FILE = "e2e_results.txt"
OUTPUT_DIR = "docs/assets"

if not os.path.exists(OUTPUT_DIR):
    os.makedirs(OUTPUT_DIR)

# Data Structure:
# exp_data[Metric][ExperimentType] = list of rows
# We will filter on the fly.

def parse_results():
    data = []
    with open(RESULTS_FILE, 'r') as f:
        reader = csv.reader(f)
        for row in reader:
            if len(row) < 7: continue
            # Format: 'Metric', 'Mode', 'ClusterSize', 'TxCount', 'DepRate', 'Threads', 'Value'
            # Example: Throughput,Original,1,1000,0.4,32,865.50
            try:
                item = {
                    'Metric': row[0].strip(),
                    'Mode': row[1].strip(),
                    'ClusterSize': int(row[2]),
                    'TxCount': int(row[3]),
                    'DepRate': float(row[4]),
                    'Threads': int(row[5]),
                    'Value': float(row[6])
                }
                data.append(item)
            except ValueError:
                continue
    return data

data = parse_results()

def get_series(metric, fixed_params, x_param):
    # fixed_params is dict of key->value constraints
    # x_param is the key to use for X-axis
    # Returns (x_values, y_values) sorted by x
    filtered = []
    for row in data:
        if row['Metric'] != metric: continue
        match = True
        for k, v in fixed_params.items():
            if k == 'Mode' and row['Mode'] != v: match = False
            if k == 'ClusterSize' and row['ClusterSize'] != v: match = False
            # Float comparison for DepRate
            if k == 'DepRate' and abs(row['DepRate'] - v) > 0.001: match = False
            if k == 'Threads' and row['Threads'] != v: match = False
            if k == 'TxCount' and row['TxCount'] != v: match = False
        
        if match:
            filtered.append((row[x_param], row['Value']))
    
    filtered.sort(key=lambda x: x[0])
    return [x[0] for x in filtered], [x[1] for x in filtered]

def plot_graph(title, xlabel, ylabel, filename, x_key, metric, fixed_common):
    plt.figure(figsize=(10, 6))
    
    # Define curves requested:
    # 1. Original Fabric (Cluster 1 or 5? - Use 5 to show baseline latency overhead, or 1? Original is consistently ~850 regardless of cluster in committer logic, but let's use Cluster 1 as baseline if available, or just 'Original' mode)
    # Actually, E2E results have Original at Cluster 1, 3, 5. Let's use Cluster 5 for "Original" to match "Proposed (Cluster 5)".
    
    # Curves:
    # - Original (Cluster 5)
    # - Proposed (Cluster 5)
    # - Proposed (Cluster 1)
    
    configs = [
        ('Original Fabric', {'Mode': 'Original', 'ClusterSize': 5}, 'blue', 'o-'),
        ('Proposed Fabric (Cluster 5)', {'Mode': 'Proposed', 'ClusterSize': 5}, 'green', 's-'),
        ('Proposed Fabric (Cluster 1)', {'Mode': 'Proposed', 'ClusterSize': 1}, 'red', '^-'),
    ]

    # For Cluster Size plot, x_key is 'ClusterSize', so we don't fix ClusterSize in configs.
    if x_key == 'ClusterSize':
        configs = [
            ('Original Fabric', {'Mode': 'Original'}, 'blue', 'o-'),
            ('Proposed Fabric', {'Mode': 'Proposed'}, 'green', 's-'),
        ]

    for label, params, color, style in configs:
        # Merge fixed_common into params
        query_params = {**fixed_common, **params}
        
        # If x_key is in params (e.g. ClusterSize), remove it so we don't filter by it
        if x_key in query_params:
            del query_params[x_key]
            
        x, y = get_series(metric, query_params, x_key)
        if x and y:
             plt.plot(x, y, style, label=label, color=color)

    plt.title(title)
    plt.xlabel(xlabel)
    plt.ylabel(ylabel)
    plt.grid(True)
    plt.legend()
    plt.savefig(os.path.join(OUTPUT_DIR, filename))
    plt.close()

# 1. Throughput vs Tx Count
# Fixed: Dep=0.4, Threads=32
plot_graph(
    "Throughput vs Transactions", "Number of Transactions", "Throughput (TPS)",
    "throughput_vs_tx.png", "TxCount", "Throughput",
    {'DepRate': 0.4, 'Threads': 32}
)

# 2. Reject Rate vs Tx Count
plot_graph(
    "Reject Rate vs Transactions", "Number of Transactions", "Reject Rate",
    "reject_vs_tx.png", "TxCount", "RejectRate",
    {'DepRate': 0.4, 'Threads': 32}
)

# 3. Throughput vs Dependency
# Fixed: Tx=1000, Threads=32
plot_graph(
    "Throughput vs Dependency", "Dependency Rate", "Throughput (TPS)",
    "throughput_vs_dep.png", "DepRate", "Throughput",
    {'TxCount': 1000, 'Threads': 32}
)

# 4. Reject Rate vs Dependency
plot_graph(
    "Reject Rate vs Dependency", "Dependency Rate", "Reject Rate",
    "reject_vs_dep.png", "DepRate", "RejectRate",
    {'TxCount': 1000, 'Threads': 32}
)

# 5. Throughput vs Threads
# Fixed: Tx=1000, Dep=0.4
plot_graph(
    "Throughput vs Threads", "Number of Threads", "Throughput (TPS)",
    "throughput_vs_threads.png", "Threads", "Throughput",
    {'TxCount': 1000, 'DepRate': 0.4}
)

# 6. Reject Rate vs Threads
plot_graph(
    "Reject Rate vs Threads", "Number of Threads", "Reject Rate",
    "reject_vs_threads.png", "Threads", "RejectRate",
    {'TxCount': 1000, 'DepRate': 0.4}
)

# 7. Throughput vs Cluster Size (Exp 4)
# Fixed: Tx=1000, Dep=0.4, Threads=32
plot_graph(
    "Throughput vs Cluster Size", "Cluster Size", "Throughput (TPS)",
    "throughput_vs_cluster.png", "ClusterSize", "Throughput",
    {'TxCount': 1000, 'DepRate': 0.4, 'Threads': 32}
)

# 8. Reject Rate vs Cluster Size
plot_graph(
    "Reject Rate vs Cluster Size", "Cluster Size", "Reject Rate",
    "reject_vs_cluster.png", "ClusterSize", "RejectRate",
    {'TxCount': 1000, 'DepRate': 0.4, 'Threads': 32}
)

# 9. Response Time vs Tx Count
# Fixed: Dep=0.4, Threads=32
# Note: Metric name in results is ResponseTime
plot_graph(
    "Average Response Time vs Transactions", "Number of Transactions", "Response Time (s)",
    "latency_vs_tx.png", "TxCount", "ResponseTime",
    {'DepRate': 0.4, 'Threads': 32}
)

print("Plots generated in docs/assets/")
