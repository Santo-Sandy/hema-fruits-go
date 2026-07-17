package adminsubscription

import (
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func buildFcmPipeline(senderId interface{}, roles []string, businessType []string) bson.A {
	return bson.A{

		//  Match users
		bson.D{
			{"$match", bson.D{
				{"_id", bson.D{{"$ne", senderId}}},
				{"role", bson.D{{"$in", roles}}},
				{"businessType", bson.D{{"$in", businessType}}},
			}},
		},
		bson.D{
			{"$lookup", bson.D{
				{"from", "blocked"},
				{"let", bson.D{
					{"receiverId", "$_id"},
				}},
				{"pipeline", bson.A{

					bson.D{
						{"$match", bson.D{
							{"$expr", bson.D{
								{"$or", bson.A{

									// sender blocked receiver
									bson.D{
										{"$and", bson.A{
											bson.D{
												{"$eq", bson.A{
													"$userId",
													senderId,
												}},
											},
											bson.D{
												{"$eq", bson.A{
													"$block_id",
													"$$receiverId",
												}},
											},
										}},
									},

									// receiver blocked sender
									bson.D{
										{"$and", bson.A{
											bson.D{
												{"$eq", bson.A{
													"$userId",
													"$$receiverId",
												}},
											},
											bson.D{
												{"$eq", bson.A{
													"$block_id",
													senderId,
												}},
											},
										}},
									},
								}},
							}},
						}},
					},
				}},
				{"as", "blockedRelation"},
			}},
		},
		//  Filter blocked users
		bson.D{
			{"$match", bson.D{
				{"blockedRelation", bson.D{
					{"$size", 0},
				}},
			}},
		},
		//  Lookup active devices
		bson.D{
			{"$lookup", bson.D{
				{"from", "user_device_history"},
				{"localField", "_id"},
				{"foreignField", "user_id"},
				{"pipeline", bson.A{
					bson.D{
						{"$match", bson.D{
							{"session_closed", false},
							{"fcm_token", bson.D{
								{"$nin", bson.A{
									"",
									nil,
								}},
							}},
						}},
					},
					bson.D{
						{"$group", bson.D{
							{"_id", nil},
							{"fcmtokens", bson.D{
								{"$addToSet", "$fcm_token"},
							}},
						}},
					},
				}},
				{"as", "deviceData"},
			}},
		},

		//  Unwind
		bson.D{
			{"$unwind", bson.D{
				{"path", "$deviceData"},
				{"preserveNullAndEmptyArrays", false},
			}},
		},

		//  Project tokens
		bson.D{
			{"$project", bson.D{
				{"receiverId", "$_id"},
				{"fcmtokens", "$deviceData.fcmtokens"},
			}},
		},
	}
}

func buildResponseNotificationPipeline(receiverId interface{}, responderId interface{}) bson.A {

	return bson.A{
		bson.D{{"$match", bson.D{{"_id", receiverId}}}},
		bson.D{
			{"$lookup", bson.D{
				{"from", "blocked"},
				{"let", bson.D{
					{"receiverId", "$_id"},
				}},
				{"pipeline", bson.A{

					bson.D{
						{"$match", bson.D{
							{"$expr", bson.D{
								{"$or", bson.A{

									// sender blocked receiver
									bson.D{
										{"$and", bson.A{
											bson.D{
												{"$eq", bson.A{
													"$userId",
													responderId,
												}},
											},
											bson.D{
												{"$eq", bson.A{
													"$block_id",
													"$$receiverId",
												}},
											},
										}},
									},

									// receiver blocked sender
									bson.D{
										{"$and", bson.A{
											bson.D{
												{"$eq", bson.A{
													"$userId",
													"$$receiverId",
												}},
											},
											bson.D{
												{"$eq", bson.A{
													"$block_id",
													responderId,
												}},
											},
										}},
									},
								}},
							}},
						}},
					},
				}},
				{"as", "blockedRelation"},
			}},
		},

		bson.D{
			{"$match", bson.D{
				{"blockedRelation", bson.D{
					{"$size", 0},
				}},
			}},
		},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "user_device_history"},
					{"localField", "_id"},
					{"foreignField", "user_id"},
					{"pipeline",
						bson.A{
							bson.D{
								{"$match",
									bson.D{
										{"session_closed", false},
										{"fcm_token",
											bson.D{
												{"$nin",
													bson.A{
														primitive.Null{},
														"",
													},
												},
											},
										},
									},
								},
							},
							bson.D{
								{"$group",
									bson.D{
										{"_id", primitive.Null{}},
										{"fcmtokens", bson.D{{"$addToSet", "$fcm_token"}}},
									},
								},
							},
						},
					},
					{"as", "deviceData"},
				},
			},
		},
		bson.D{
			{"$unwind",
				bson.D{
					{"path", "$deviceData"},
					{"preserveNullAndEmptyArrays", false},
				},
			},
		},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "users"},
					{"let", bson.D{{"responderId", responderId}}},
					{"pipeline",
						bson.A{
							bson.D{
								{"$match",
									bson.D{
										{"$expr",
											bson.D{
												{"$eq",
													bson.A{
														"$_id",
														"$$responderId",
													},
												},
											},
										},
									},
								},
							},
							bson.D{
								{"$project",
									bson.D{
										{"_id", 0},
										{"responderName", "$name"},
									},
								},
							},
						},
					},
					{"as", "responderData"},
				},
			},
		},
		bson.D{{"$unwind", "$responderData"}},
		bson.D{
			{"$project",
				bson.D{
					{"receiverId", "$_id"},
					{"responderName", "$responderData.responderName"},
					{"fcmtokens", "$deviceData.fcmtokens"},
				},
			},
		},
	}
}
