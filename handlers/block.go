package handlers

import (
	"encoding/hex"
	"eth2-exporter/db"
	"eth2-exporter/services"
	"eth2-exporter/types"
	"eth2-exporter/utils"
	"fmt"
	"github.com/gorilla/mux"
	"html/template"
	"net/http"
	"strings"
	"time"
)

var blockTemplate = template.Must(template.New("block").Funcs(utils.GetTemplateFuncs()).ParseFiles("templates/layout.html", "templates/block.html"))
var blockNotFoundTemplate = template.Must(template.New("blocknotfound").ParseFiles("templates/layout.html", "templates/blocknotfound.html"))

func Block(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	vars := mux.Vars(r)
	slotOrHash := strings.Replace(vars["slotOrHash"], "0x", "", -1)

	blockPageData := types.BlockPageData{}

	blockRootHash, err := hex.DecodeString(slotOrHash)

	if err == nil && len(slotOrHash) == 64 {
		slotOrHash = "-1"
	}

	err = db.DB.Get(&blockPageData, `
	SELECT 
			epoch, 
			slot, 
			blockroot, 
			parentroot, 
			stateroot, 
			signature, 
			randaoreveal, 
			graffiti, 
			eth1data_depositroot, 
			eth1data_depositcount, 
			eth1data_blockhash, 
			proposerslashingscount, 
			attesterslashingscount,
			attestationscount, 
			depositscount, 
			voluntaryexitscount, 
			proposer,
			status   
	FROM blocks 
	WHERE slot = $1 OR blockroot = $2 ORDER BY status desc LIMIT 1`,
		slotOrHash, blockRootHash)

	data := &types.PageData{
		Meta: &types.Meta{
			Description: "beaconcha.in makes the Ethereum 2.0. beacon chain accessible to non-technical end users",
		},
		ShowSyncingMessage: services.IsSyncing(),
		Active:             "blocks",
		Data:               nil,
	}

	if err != nil {
		data.Meta.Title = fmt.Sprintf("Slot %v - beaconcha.in - Ethereum 2.0 beacon chain explorer - %v", slotOrHash, time.Now().Year())
		data.Meta.Path = "/block/" + slotOrHash
		logger.Printf("Error retrieving block data: %v", err)
		err = blockNotFoundTemplate.ExecuteTemplate(w, "layout", data)

		if err != nil {
			logger.Fatalf("Error executing template for %v route: %v", r.URL.String(), err)
		}
		return
	}

	data.Meta.Title = fmt.Sprintf("Slot %v - beaconcha.in - Ethereum 2.0 beacon chain explorer - %v", blockPageData.Slot, time.Now().Year())
	data.Meta.Path = fmt.Sprintf("/block/%v", blockPageData.Slot)

	blockPageData.Ts = utils.SlotToTime(blockPageData.Slot)
	blockPageData.NextSlot = blockPageData.Slot + 1
	blockPageData.PreviousSlot = blockPageData.Slot - 1
	blockPageData.SlashingsCount = blockPageData.AttesterSlashingsCount + blockPageData.ProposerSlashingsCount

	slots := types.BlockPageMinMaxSlot{}
	err = db.DB.Get(&slots, "SELECT MAX(slot) AS maxslot, MIN(slot) as minslot FROM blocks")
	if err != nil {
		logger.Printf("Error retrieving block data: %v", err)
		http.Error(w, "Internal server error", 503)
		return
	}

	if blockPageData.NextSlot > slots.MaxSlot {
		blockPageData.NextSlot = 0
	}

	if blockPageData.PreviousSlot < slots.MinSlot {
		blockPageData.PreviousSlot = 0
	}

	var attestations []*types.BlockPageAttestation
	rows, err := db.DB.Query(`SELECT    block_slot,
											 block_index,
											 aggregationbits, 
											 validators, 
											 signature, 
											 slot, 
											 committeeindex, 
											 beaconblockroot, 
											 source_epoch, 
											 source_root, 
											 target_epoch, 
											 target_root 
										FROM blocks_attestations 
												WHERE block_slot = $1 
												ORDER BY block_index`,
		blockPageData.Slot)
	if err != nil {
		logger.Printf("Error retrieving block attestation data: %v", err)
		http.Error(w, "Internal server error", 503)
		return
	}
	defer rows.Close()

	for rows.Next() {
		attestation := &types.BlockPageAttestation{}

		err := rows.Scan(
			&attestation.BlockSlot,
			&attestation.BlockIndex,
			&attestation.AggregationBits,
			&attestation.Validators,
			&attestation.Signature,
			&attestation.Slot,
			&attestation.CommitteeIndex,
			&attestation.BeaconBlockRoot,
			&attestation.SourceEpoch,
			&attestation.SourceRoot,
			&attestation.TargetEpoch,
			&attestation.TargetRoot)
		if err != nil {
			logger.Printf("Error scanning block attestation data: %v", err)
			http.Error(w, "Internal server error", 503)
			return
		}
		attestations = append(attestations, attestation)
	}
	blockPageData.Attestations = attestations

	var votes []*types.BlockPageAttestation
	rows, err = db.DB.Query(`SELECT    block_slot,
											 block_index,
											 aggregationbits, 
											 validators, 
											 signature, 
											 slot, 
											 committeeindex, 
											 beaconblockroot, 
											 source_epoch, 
											 source_root, 
											 target_epoch, 
											 target_root 
										FROM blocks_attestations 
										WHERE beaconblockroot = $1 
										ORDER BY committeeindex`,
		blockPageData.BlockRoot)
	if err != nil {
		logger.Printf("Error retrieving block votes data: %v", err)
		http.Error(w, "Internal server error", 503)
		return
	}
	defer rows.Close()

	for rows.Next() {
		attestation := &types.BlockPageAttestation{}

		err := rows.Scan(
			&attestation.BlockSlot,
			&attestation.BlockIndex,
			&attestation.AggregationBits,
			&attestation.Validators,
			&attestation.Signature,
			&attestation.Slot,
			&attestation.CommitteeIndex,
			&attestation.BeaconBlockRoot,
			&attestation.SourceEpoch,
			&attestation.SourceRoot,
			&attestation.TargetEpoch,
			&attestation.TargetRoot)
		if err != nil {
			logger.Printf("Error scanning block votes data: %v", err)
			http.Error(w, "Internal server error", 503)
			return
		}
		votes = append(votes, attestation)
	}
	blockPageData.Votes = votes

	var deposits []*types.BlockPageDeposit
	err = db.DB.Select(&deposits, `SELECT publickey, 
												     withdrawalcredentials, 
												     amount, 
												     signature
												FROM blocks_deposits 
												WHERE block_slot = $1 
												ORDER BY block_index`,
		blockPageData.Slot)
	if err != nil {
		logger.Printf("Error retrieving block deposit data: %v", err)
		http.Error(w, "Internal server error", 503)
		return
	}

	for _, d := range deposits {
		d.AmountFormatted = utils.FormatBalance(d.Amount)
	}
	blockPageData.Deposits = deposits

	data.Data = blockPageData

	err = blockTemplate.ExecuteTemplate(w, "layout", data)

	if err != nil {
		logger.Fatalf("Error executing template for %v route: %v", r.URL.String(), err)
	}
}
