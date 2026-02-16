var selectedUser = null;

function selectUser(username) {
    selectedUser = username;
    document.querySelectorAll('.member-card').forEach(function(card) {
        card.classList.toggle('selected', card.dataset.username === username);
    });
    document.getElementById('selectedName').textContent = username;
    document.getElementById('actionBar').style.display = 'flex';
    document.getElementById('reasonPanel').style.display = 'none';
}

function toggleReasonPanel() {
    var panel = document.getElementById('reasonPanel');
    panel.style.display = panel.style.display === 'none' ? 'block' : 'none';
}

function submitStar(reason) {
    if (!reason || !selectedUser) return;
    var form = document.createElement("form");
    form.method = "POST";
    form.action = "/star";
    var u = document.createElement("input");
    u.type = "hidden"; u.name = "username"; u.value = selectedUser;
    var r = document.createElement("input");
    r.type = "hidden"; r.name = "reason"; r.value = reason;
    form.appendChild(u);
    form.appendChild(r);
    document.body.appendChild(form);
    form.submit();
}

function redeemReward(rewardId, rewardName, cost) {
    if (!selectedUser) {
        alert("Please select a family member first.");
        return;
    }
    if (!confirm("Spend " + cost + " stars for " + selectedUser + " on \"" + rewardName + "\"?")) return;
    var form = document.createElement("form");
    form.method = "POST";
    form.action = "/redeem";
    var ri = document.createElement("input");
    ri.type = "hidden"; ri.name = "reward_id"; ri.value = rewardId;
    var u = document.createElement("input");
    u.type = "hidden"; u.name = "username"; u.value = selectedUser;
    form.appendChild(ri);
    form.appendChild(u);
    document.body.appendChild(form);
    form.submit();
}

function deleteKey(id) {
    if (!confirm("Revoke this API key?")) return;
    fetch("/admin/apikey/" + id, { method: "DELETE" })
        .then(function() { location.reload(); });
}
